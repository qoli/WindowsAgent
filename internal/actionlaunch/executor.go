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
	"github.com/qoli/WindowsAgent/internal/ocraction"
	"github.com/qoli/WindowsAgent/internal/ocrworker"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
)

type Executor struct {
	rules       *rules.Store
	observation scriptlaunch.Executor
	capturer    capture.RegionCapturer
	ocr         ocrworker.Recognizer
	foreground  func() (foreground.Info, error)
}

func New(
	ruleStore *rules.Store,
	observation scriptlaunch.Executor,
	capturer capture.RegionCapturer,
	ocr ocrworker.Recognizer,
	foregroundSnapshot func() (foreground.Info, error),
) (*Executor, error) {
	if ruleStore == nil || observation == nil || capturer == nil || ocr == nil || foregroundSnapshot == nil {
		return nil, errors.New("Rule store, observation executor, region capturer, OCR recognizer, and foreground resolver are required")
	}
	return &Executor{
		rules: ruleStore, observation: observation, capturer: capturer, ocr: ocr,
		foreground: foregroundSnapshot,
	}, nil
}

func (e *Executor) Run(ctx context.Context, invocation scriptlaunch.Invocation) (json.RawMessage, error) {
	if e == nil {
		return nil, errors.New("Action executor is required")
	}
	action, err := e.rules.ResolveAction(invocation.Capability)
	if err != nil {
		return nil, fmt.Errorf("resolve Action %q: %w", invocation.Capability, err)
	}
	switch action.Runtime {
	case rules.ObservationRuntimeV1:
		return e.observation.Run(ctx, invocation)
	case rules.PpOcrActionRuntimeV1:
		return e.runOCR(ctx, action, invocation.Inputs)
	default:
		return nil, fmt.Errorf("Action %q declares unsupported runtime %q", action.ID, action.Runtime)
	}
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
		"ok":         true,
		"capability": action.ID,
		"runtime":    action.Runtime,
		"result": map[string]any{
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
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode OCR Action result: %w", err)
	}
	return encoded, nil
}

func sameForeground(before, captured foreground.Info) bool {
	return before.ProcessID == captured.ProcessID &&
		strings.EqualFold(before.ExecutableName, captured.ExecutableName) &&
		strings.EqualFold(filepath.Clean(before.ExecutablePath), filepath.Clean(captured.ExecutablePath))
}
