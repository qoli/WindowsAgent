// Package actionlaunch dispatches finite Actions to their declared runtime.
package actionlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/ocraction"
	"github.com/qoli/WindowsAgent/internal/ocrregionsaction"
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
	case rules.PpOcrTextRegionsActionRuntimeV1:
		output, err = e.runOCRTextRegions(ctx, action, invocation.Inputs)
		if err != nil {
			return Result{}, err
		}
	case rules.WindowsKeyActionRuntimeV1:
		pkg, err := inputaction.Load(action.Root)
		if err != nil {
			return Result{}, fmt.Errorf("load Windows key Action %q: %w", action.ID, err)
		}
		output, err = e.input.Run(ctx, pkg, invocation.Inputs, action.RuleID)
		if err != nil {
			return Result{}, err
		}
	case rules.CompositeActionRuntimeV1:
		pkg, err := streamaction.Load(action.Root)
		if err != nil {
			return Result{}, fmt.Errorf("load composite Action %q: %w", action.ID, err)
		}
		output, err = (streamaction.Runner{}).Run(
			ctx,
			pkg,
			invocation.Inputs,
			childCaller{executor: e, parent: action},
			denyCompositeEvents{},
		)
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
	if actionID == c.parent.ID {
		return nil, fmt.Errorf("child Action %q cannot call itself", actionID)
	}
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

type denyCompositeEvents struct{}

func (denyCompositeEvents) Emit(context.Context, string, json.RawMessage) (eventstream.Event, error) {
	return eventstream.Event{}, errors.New("composite Actions cannot emit streaming events")
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
		CharacterConstraint: ocrworker.CharacterConstraint(config.CharacterConstraint),
	})
	if err != nil {
		return nil, fmt.Errorf("run resident OCR profile %s: %w", action.RuntimeProfile, err)
	}
	response := map[string]any{
		"schemaVersion": 1,
		"text":          result.Text,
		"confidence":    result.Confidence,
		"decoding":      result.Decoding,
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

func (e *Executor) runOCRTextRegions(ctx context.Context, action rules.Action, inputs map[string]any) (json.RawMessage, error) {
	if inputs == nil {
		return nil, errors.New("OCR text regions Action inputs object is required")
	}
	if len(inputs) != 0 {
		return nil, errors.New("OCR text regions Action inputs must be an empty object")
	}
	before, err := e.foreground()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground before OCR text regions Action: %w", err)
	}
	if !strings.EqualFold(before.ExecutableName, action.RuleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", before.ExecutableName, action.RuleID)
	}
	config, err := ocrregionsaction.Load(action.Root)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	region, err := e.capturer.CaptureRegion(ctx, capture.RegionRequest{
		Region: config.ReferenceRegion, Sampling: config.Sampling, MaxPixels: config.MaxPixels,
	})
	if err != nil {
		return nil, fmt.Errorf("capture OCR text regions Action region: %w", err)
	}
	captureMS := float64(time.Since(started).Microseconds()) / 1000
	if !sameForeground(before, region.Foreground) || !strings.EqualFold(region.Foreground.ExecutableName, action.RuleID) {
		return nil, errors.New("foreground process changed during OCR text regions Action capture")
	}
	if region.ImageWidth <= 0 || region.ImageHeight <= 0 || len(region.Pixels) != region.ImageWidth*region.ImageHeight {
		return nil, errors.New("OCR text regions Action captured an invalid pixel region")
	}
	rgb := make([]byte, len(region.Pixels)*3)
	for index, pixel := range region.Pixels {
		rgb[index*3] = byte(pixel >> 16)
		rgb[index*3+1] = byte(pixel >> 8)
		rgb[index*3+2] = byte(pixel)
	}
	capturedAt := region.Foreground.ObservedAt.UTC()
	identity := capturedAt.Format("20060102T150405.000000000Z")
	result, err := e.ocr.DetectTextRegions(ctx, action.RuleID, action.RuntimeProfile, ocrworker.Request{
		RequestID: "ocr-regions-action-" + identity, ArtifactID: "screen-region-" + identity,
		CapturedAt: capturedAt, Width: region.ImageWidth, Height: region.ImageHeight, RGB: rgb,
	})
	if err != nil {
		return nil, fmt.Errorf("run resident OCR text regions profile %s: %w", action.RuntimeProfile, err)
	}
	regions := make([]map[string]any, 0, len(result.Regions))
	for _, detected := range result.Regions {
		points := make([]map[string]any, 0, len(detected.Points))
		referencePoints := make([]map[string]any, 0, len(detected.Points))
		for _, point := range detected.Points {
			points = append(points, map[string]any{"x": point.X, "y": point.Y})
			referencePoints = append(referencePoints, map[string]any{
				"x": float64(config.ReferenceRegion.X) + point.X*float64(config.ReferenceRegion.Width)/float64(region.ImageWidth),
				"y": float64(config.ReferenceRegion.Y) + point.Y*float64(config.ReferenceRegion.Height)/float64(region.ImageHeight),
			})
		}
		leftContext, err := buildLeftContext(region, config, detected.Points)
		if err != nil {
			return nil, fmt.Errorf("build OCR text region left context: %w", err)
		}
		regions = append(regions, map[string]any{
			"points": points, "referencePoints": referencePoints,
			"detectionConfidence": detected.DetectionConfidence,
			"text":                detected.Text, "recognitionConfidence": detected.RecognitionConfidence,
			"leftContext": leftContext,
		})
	}
	response := map[string]any{
		"schemaVersion": 1,
		"regions":       regions,
		"evidence": map[string]any{
			"artifactId": result.Evidence.ArtifactID, "capturedAt": result.Evidence.CapturedAt,
			"rgbSha256": result.Evidence.RGBSHA256,
			"frame": map[string]any{
				"width": region.FrameWidth, "height": region.FrameHeight,
				"foreground": map[string]any{"processId": region.Foreground.ProcessID, "executableName": region.Foreground.ExecutableName},
			},
			"coordinateSpace": map[string]any{"width": capture.ReferenceWidth, "height": capture.ReferenceHeight, "fit": "centered-16:9"},
			"referenceRegion": config.ReferenceRegion, "physicalRegion": region.PhysicalRegion,
			"image": map[string]any{"width": region.ImageWidth, "height": region.ImageHeight, "encoding": "rgb24"},
		},
		"models": result.Models,
		"timing": map[string]any{
			"captureMs":                captureMS,
			"detectionPreprocessMs":    result.Timing.DetectionPreprocessMS,
			"detectionInferenceMs":     result.Timing.DetectionInferenceMS,
			"detectionPostprocessMs":   result.Timing.DetectionPostprocessMS,
			"recognitionPreprocessMs":  result.Timing.RecognitionPreprocessMS,
			"recognitionInferenceMs":   result.Timing.RecognitionInferenceMS,
			"recognitionPostprocessMs": result.Timing.RecognitionPostprocessMS,
			"ocrTotalMs":               result.Timing.TotalMS,
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode OCR text regions Action result: %w", err)
	}
	return encoded, nil
}

func buildLeftContext(region capture.RegionResult, config ocrregionsaction.Config, points []ocrworker.TextRegionPoint) (map[string]any, error) {
	if len(points) != 4 {
		return nil, errors.New("detected text region must contain exactly four points")
	}
	minimumX := float64(region.ImageWidth)
	minimumY := float64(region.ImageHeight)
	maximumY := 0.0
	for _, point := range points {
		minimumX = min(minimumX, point.X)
		minimumY = min(minimumY, point.Y)
		maximumY = max(maximumY, point.Y)
	}
	scaleX := float64(region.ImageWidth) / float64(config.ReferenceRegion.Width)
	scaleY := float64(region.ImageHeight) / float64(config.ReferenceRegion.Height)
	left := max(0, int(minimumX)-int(math.Ceil(float64(config.LeftContextWidth)*scaleX)))
	right := min(region.ImageWidth, int(math.Ceil(minimumX)))
	top := max(0, int(math.Floor(minimumY))-int(math.Ceil(float64(config.VerticalPadding)*scaleY)))
	bottom := min(region.ImageHeight, int(math.Ceil(maximumY))+int(math.Ceil(float64(config.VerticalPadding)*scaleY)))
	if right <= left || bottom <= top {
		return map[string]any{
			"x": max(0, min(region.ImageWidth, right)), "y": max(0, min(region.ImageHeight, top)),
			"w": 0, "h": 0, "pixels": []uint32{},
			"referenceRegion": map[string]any{
				"x": float64(config.ReferenceRegion.X) + float64(max(0, min(region.ImageWidth, right)))*float64(config.ReferenceRegion.Width)/float64(region.ImageWidth),
				"y": float64(config.ReferenceRegion.Y) + float64(max(0, min(region.ImageHeight, top)))*float64(config.ReferenceRegion.Height)/float64(region.ImageHeight),
				"w": 0.0, "h": 0.0,
			},
		}, nil
	}
	width := right - left
	height := bottom - top
	pixels := make([]uint32, 0, width*height)
	for y := top; y < bottom; y++ {
		start := y*region.ImageWidth + left
		pixels = append(pixels, region.Pixels[start:start+width]...)
	}
	return map[string]any{
		"x": left, "y": top, "w": width, "h": height, "pixels": pixels,
		"referenceRegion": map[string]any{
			"x": float64(config.ReferenceRegion.X) + float64(left)*float64(config.ReferenceRegion.Width)/float64(region.ImageWidth),
			"y": float64(config.ReferenceRegion.Y) + float64(top)*float64(config.ReferenceRegion.Height)/float64(region.ImageHeight),
			"w": float64(width) * float64(config.ReferenceRegion.Width) / float64(region.ImageWidth),
			"h": float64(height) * float64(config.ReferenceRegion.Height) / float64(region.ImageHeight),
		},
	}, nil
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
