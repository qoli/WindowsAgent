// Package actionlaunch dispatches finite Actions to their declared runtime.
package actionlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/ocraction"
	"github.com/qoli/WindowsAgent/internal/ocrworker"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/streamaction"
)

type Executor struct {
	rules       *rules.Store
	observation scriptlaunch.Executor
	capturer    capture.RegionCapturer
	ocr         ocrworker.Recognizer
	foreground  func() (foreground.Info, error)
	input       InputExecutor
}

type InputExecutor interface {
	Run(context.Context, *inputaction.Package, map[string]any, string) (json.RawMessage, error)
}

type Result struct {
	ActionID string          `json:"actionId"`
	RuleID   string          `json:"ruleId"`
	Runtime  string          `json:"runtime"`
	Output   json.RawMessage `json:"output"`
}

func New(
	ruleStore *rules.Store,
	observation scriptlaunch.Executor,
	capturer capture.RegionCapturer,
	ocr ocrworker.Recognizer,
	input InputExecutor,
	foregroundSnapshot func() (foreground.Info, error),
) (*Executor, error) {
	if ruleStore == nil || observation == nil || capturer == nil || ocr == nil || input == nil || foregroundSnapshot == nil {
		return nil, errors.New("Rule store, observation executor, region capturer, OCR recognizer, input executor, and foreground resolver are required")
	}
	return &Executor{
		rules: ruleStore, observation: observation, capturer: capturer, ocr: ocr,
		foreground: foregroundSnapshot, input: input,
	}, nil
}

func (e *Executor) Run(ctx context.Context, invocation scriptlaunch.Invocation) (json.RawMessage, error) {
	result, err := e.RunAction(ctx, invocation)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (e *Executor) RunAction(ctx context.Context, invocation scriptlaunch.Invocation) (Result, error) {
	if e == nil {
		return Result{}, errors.New("Action executor is required")
	}
	action, err := e.rules.ResolveAction(invocation.Capability)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Action %q: %w", invocation.Capability, err)
	}
	if action.Execution.Completion != rules.CompletionReturn {
		return Result{}, fmt.Errorf("Action %q does not declare return completion", action.ID)
	}
	var output json.RawMessage
	switch action.Runtime {
	case rules.ObservationRuntimeV1:
		raw, err := e.observation.Run(ctx, invocation)
		if err != nil {
			return Result{}, err
		}
		output, err = observationOutput(raw)
		if err != nil {
			return Result{}, fmt.Errorf("decode observation Action output: %w", err)
		}
	case rules.PpOcrActionRuntimeV1:
		output, err = e.runOCR(ctx, action, invocation.Inputs)
		if err != nil {
			return Result{}, err
		}
	case rules.FrontierKeyActionRuntimeV1:
		pkg, err := inputaction.Load(action.Root)
		if err != nil {
			return Result{}, fmt.Errorf("load Frontier key Action %q: %w", action.ID, err)
		}
		output, err = e.input.Run(ctx, pkg, invocation.Inputs, action.RuleID)
		if err != nil {
			return Result{}, err
		}
	default:
		return Result{}, fmt.Errorf("Action %q declares unsupported runtime %q", action.ID, action.Runtime)
	}
	return Result{ActionID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime, Output: output}, nil
}

func (e *Executor) RunStreaming(ctx context.Context, invocation scriptlaunch.Invocation, reporter streamaction.Reporter) (Result, error) {
	if e == nil || ctx == nil || reporter == nil {
		return Result{}, errors.New("Action executor, context, and streaming reporter are required")
	}
	action, err := e.rules.ResolveAction(invocation.Capability)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Action %q: %w", invocation.Capability, err)
	}
	if action.Execution.Completion != rules.CompletionStream {
		return Result{}, fmt.Errorf("Action %q does not declare stream completion", action.ID)
	}
	if action.Runtime != rules.StreamingActionRuntimeV1 {
		return Result{}, fmt.Errorf("streaming Action %q declares unsupported runtime %q", action.ID, action.Runtime)
	}
	pkg, err := streamaction.Load(action.Root)
	if err != nil {
		return Result{}, fmt.Errorf("load streaming Action %q: %w", action.ID, err)
	}
	output, err := (streamaction.Runner{}).Run(ctx, pkg, invocation.Inputs, childCaller{executor: e, parent: action}, reporter)
	if err != nil {
		return Result{}, err
	}
	return Result{ActionID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime, Output: output}, nil
}

type childCaller struct {
	executor *Executor
	parent   rules.Action
}

func (c childCaller) Call(ctx context.Context, actionID string, inputs map[string]any) (json.RawMessage, error) {
	child, err := c.executor.rules.ResolveAction(actionID)
	if err != nil {
		return nil, fmt.Errorf("resolve child Action %q: %w", actionID, err)
	}
	if !strings.EqualFold(child.RuleID, c.parent.RuleID) {
		return nil, fmt.Errorf("child Action %q belongs to Rule %q, expected %q", child.ID, child.RuleID, c.parent.RuleID)
	}
	if child.Execution.Completion != rules.CompletionReturn {
		return nil, fmt.Errorf("child Action %q must declare return completion", child.ID)
	}
	result, err := c.executor.RunAction(ctx, scriptlaunch.Invocation{Capability: actionID, Inputs: inputs})
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), result.Output...), nil
}

func (e *Executor) runOCR(ctx context.Context, action rules.Action, inputs map[string]any) (json.RawMessage, error) {
	if inputs == nil {
		return nil, errors.New("OCR Action inputs object is required")
	}
	if len(inputs) != 0 {
		return nil, errors.New("OCR Action inputs must be an empty object")
	}
	before, err := e.foreground()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground before OCR Action: %w", err)
	}
	if !strings.EqualFold(before.ExecutableName, action.RuleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", before.ExecutableName, action.RuleID)
	}
	config, err := ocraction.Load(action.Root)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	region, err := e.capturer.CaptureRegion(ctx, capture.RegionRequest{
		Region: config.ReferenceRegion, Sampling: config.Sampling, MaxPixels: config.MaxPixels,
	})
	if err != nil {
		return nil, fmt.Errorf("capture OCR Action region: %w", err)
	}
	captureMS := float64(time.Since(started).Microseconds()) / 1000
	if !sameForeground(before, region.Foreground) || !strings.EqualFold(region.Foreground.ExecutableName, action.RuleID) {
		return nil, errors.New("foreground process changed during OCR Action capture")
	}
	if region.ImageWidth <= 0 || region.ImageHeight <= 0 || len(region.Pixels) != region.ImageWidth*region.ImageHeight {
		return nil, errors.New("OCR Action captured an invalid pixel region")
	}
	if region.ImageWidth != region.ImageHeight*10 {
		return nil, fmt.Errorf("OCR Action reference sample must preserve a 10:1 aspect ratio, got %dx%d", region.ImageWidth, region.ImageHeight)
	}
	rgb := make([]byte, len(region.Pixels)*3)
	for index, pixel := range region.Pixels {
		rgb[index*3] = byte(pixel >> 16)
		rgb[index*3+1] = byte(pixel >> 8)
		rgb[index*3+2] = byte(pixel)
	}
	capturedAt := region.Foreground.ObservedAt.UTC()
	identity := capturedAt.Format("20060102T150405.000000000Z")
	result, err := e.ocr.Recognize(ctx, action.RuleID, action.RuntimeProfile, ocrworker.Request{
		RequestID:  "ocr-action-" + identity,
		ArtifactID: "screen-region-" + identity,
		CapturedAt: capturedAt,
		Width:      region.ImageWidth, Height: region.ImageHeight, RGB: rgb,
	})
	if err != nil {
		return nil, fmt.Errorf("run resident OCR profile %s: %w", action.RuntimeProfile, err)
	}
	response := map[string]any{
		"schemaVersion": 1,
		"text":          result.Text,
		"confidence":    result.Confidence,
		"evidence": map[string]any{
			"artifactId": result.Evidence.ArtifactID,
			"capturedAt": result.Evidence.CapturedAt,
			"rgbSha256":  result.Evidence.RGBSHA256,
			"frame": map[string]any{
				"width": region.FrameWidth, "height": region.FrameHeight,
				"foreground": map[string]any{
					"processId":      region.Foreground.ProcessID,
					"executableName": region.Foreground.ExecutableName,
				},
			},
			"coordinateSpace": map[string]any{
				"width": capture.ReferenceWidth, "height": capture.ReferenceHeight, "fit": "centered-16:9",
			},
			"referenceRegion": config.ReferenceRegion,
			"physicalRegion":  region.PhysicalRegion,
			"image": map[string]any{
				"width": region.ImageWidth, "height": region.ImageHeight, "encoding": "rgb24",
			},
		},
		"model": result.Model,
		"timing": map[string]any{
			"captureMs":     captureMS,
			"preprocessMs":  result.Timing.PreprocessMS,
			"inferenceMs":   result.Timing.InferenceMS,
			"postprocessMs": result.Timing.PostprocessMS,
			"ocrTotalMs":    result.Timing.TotalMS,
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode OCR Action result: %w", err)
	}
	return encoded, nil
}

func observationOutput(raw json.RawMessage) (json.RawMessage, error) {
	var envelope struct {
		OK     bool            `json:"ok"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if !envelope.OK || len(envelope.Output) == 0 || !json.Valid(envelope.Output) {
		return nil, errors.New("observation Action did not return one valid output")
	}
	return append(json.RawMessage(nil), envelope.Output...), nil
}

func sameForeground(before, captured foreground.Info) bool {
	return before.ProcessID == captured.ProcessID &&
		strings.EqualFold(before.ExecutableName, captured.ExecutableName) &&
		strings.EqualFold(filepath.Clean(before.ExecutablePath), filepath.Clean(captured.ExecutablePath))
}
