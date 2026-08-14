// Package actionlaunch dispatches finite Actions to their declared runtime.
package actionlaunch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/puredecision"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/streamaction"
)

type Executor struct {
	rules       *rules.Store
	observation scriptlaunch.Executor
	capturer    capture.RegionCapturer
	ocr         ocrworker.Recognizer
	foreground  func() (foreground.Info, error)
	input       InputExecutor
	pointer     PointerExecutor
}

type InputExecutor interface {
	Run(context.Context, *inputaction.Package, map[string]any, string) (json.RawMessage, error)
}

type PointerExecutor interface {
	Run(context.Context, *pointeraction.Package, map[string]any, string) (json.RawMessage, error)
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
	pointer PointerExecutor,
	foregroundSnapshot func() (foreground.Info, error),
) (*Executor, error) {
	if ruleStore == nil || observation == nil || capturer == nil || ocr == nil || input == nil || pointer == nil || foregroundSnapshot == nil {
		return nil, errors.New("Rule store, observation executor, region capturer, OCR recognizer, input executor, pointer executor, and foreground resolver are required")
	}
	return &Executor{
		rules: ruleStore, observation: observation, capturer: capturer, ocr: ocr,
		foreground: foregroundSnapshot, input: input, pointer: pointer,
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
	case rules.PureDecisionRuntimeV1:
		pkg, err := scriptpackage.Load(action.Root, action.ID)
		if err != nil {
			return Result{}, fmt.Errorf("load pure decision Action %q: %w", action.ID, err)
		}
		output, err = puredecision.Run(ctx, pkg, invocation.Inputs)
		if err != nil {
			return Result{}, err
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
	case rules.WindowsPointerActionRuntimeV1:
		pkg, err := pointeraction.Load(action.Root)
		if err != nil {
			return Result{}, fmt.Errorf("load Windows pointer Action %q: %w", action.ID, err)
		}
		output, err = e.pointer.Run(ctx, pkg, invocation.Inputs, action.RuleID)
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
	output, err := (streamaction.Runner{}).Run(ctx, pkg, invocation.Inputs, childCaller{executor: e, parent: action, reporter: reporter}, reporter)
	if err != nil {
		return Result{}, err
	}
	return Result{ActionID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime, Output: output}, nil
}

type childCaller struct {
	executor *Executor
	parent   rules.Action
	reporter streamaction.Reporter
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
	if child.Execution.Completion == rules.CompletionStream {
		if c.parent.Execution.Completion != rules.CompletionStream || c.reporter == nil {
			return nil, fmt.Errorf("streaming child Action %q requires a streaming parent reporter", child.ID)
		}
		if child.Execution.Lifecycle != rules.LifecycleLinear || !child.Execution.Interruptible {
			return nil, fmt.Errorf("streaming child Action %q must be linear and interruptible", child.ID)
		}
		childExecutionID, err := newChildExecutionID(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("create streaming child execution ID: %w", err)
		}
		reporter := nestedChildReporter{parent: c.reporter, actionID: child.ID, childExecutionID: childExecutionID}
		if err := reporter.emit(ctx, "action.child.started", nil); err != nil {
			return nil, err
		}
		result, err := c.executor.RunStreaming(ctx, scriptlaunch.Invocation{Capability: actionID, Inputs: inputs}, reporter)
		if err != nil {
			if emitErr := reporter.emit(context.WithoutCancel(ctx), "action.child.failed", map[string]any{"error": err.Error()}); emitErr != nil {
				return nil, errors.Join(err, emitErr)
			}
			return nil, err
		}
		if err := reporter.emit(ctx, "action.child.completed", map[string]any{"output": json.RawMessage(result.Output)}); err != nil {
			return nil, err
		}
		return append(json.RawMessage(nil), result.Output...), nil
	}
	if child.Execution.Completion != rules.CompletionReturn {
		return nil, fmt.Errorf("child Action %q declares unsupported completion %q", child.ID, child.Execution.Completion)
	}
	result, err := c.executor.RunAction(ctx, scriptlaunch.Invocation{Capability: actionID, Inputs: inputs})
	if err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), result.Output...), nil
}

type nestedChildReporter struct {
	parent           streamaction.Reporter
	actionID         string
	childExecutionID string
}

func (r nestedChildReporter) Emit(ctx context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return eventstream.Event{}, fmt.Errorf("decode streaming child event payload: %w", err)
	}
	return r.emitEvent(ctx, "action.child.event", map[string]any{"type": eventType, "payload": decoded})
}

func (r nestedChildReporter) emit(ctx context.Context, eventType string, payload map[string]any) error {
	_, err := r.emitEvent(ctx, eventType, payload)
	return err
}

func (r nestedChildReporter) emitEvent(ctx context.Context, eventType string, payload map[string]any) (eventstream.Event, error) {
	if r.parent == nil || r.actionID == "" || r.childExecutionID == "" {
		return eventstream.Event{}, errors.New("streaming child reporter is incomplete")
	}
	envelope := map[string]any{"actionId": r.actionID, "childExecutionId": r.childExecutionID}
	for key, value := range payload {
		envelope[key] = value
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("encode streaming child event: %w", err)
	}
	event, err := r.parent.Emit(ctx, eventType, encoded)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("commit streaming child event: %w", err)
	}
	return event, nil
}

func newChildExecutionID(random io.Reader) (string, error) {
	var data [16]byte
	if _, err := io.ReadFull(random, data[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("child_%x", data[:]), nil
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
	sampling := config.Sampling
	maxPixels := config.MaxPixels
	if config.Cascade != nil {
		sampling = capture.SamplingNative
		maxPixels = config.Cascade.NativeCaptureMaxPixels
	}
	region, err := e.capturer.CaptureRegion(ctx, capture.RegionRequest{
		Region: config.ReferenceRegion, Sampling: sampling, MaxPixels: maxPixels,
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
	image, err := ocraction.ImageFromPixels(region.Pixels, region.ImageWidth, region.ImageHeight)
	if err != nil {
		return nil, fmt.Errorf("prepare OCR Action RGB region: %w", err)
	}
	if config.Cascade != nil {
		return e.runOCRCascade(ctx, action, config, region, image, started, captureMS)
	}
	rgb := image.RGB
	filteredPixelCount := 0
	if config.PixelFilter != nil {
		filteredPixelCount, err = config.PixelFilter.Apply(rgb)
		if err != nil {
			return nil, fmt.Errorf("preprocess OCR Action region: %w", err)
		}
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
			"pixelFilter":        config.PixelFilter,
			"filteredPixelCount": filteredPixelCount,
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

type cascadeAttempt struct {
	RouteID  string
	Image    ocraction.RGBImage
	Result   ocrworker.Result
	Decision json.RawMessage
	Accepted bool
	State    string
}

func (e *Executor) runOCRCascade(
	ctx context.Context,
	action rules.Action,
	config ocraction.Config,
	region capture.RegionResult,
	native ocraction.RGBImage,
	started time.Time,
	captureMS float64,
) (json.RawMessage, error) {
	cascade := config.Cascade
	if cascade == nil {
		return nil, errors.New("OCR cascade configuration is required")
	}
	resizeStarted := time.Now()
	reference, err := ocraction.ResizeHalfPixel(native, config.ReferenceRegion.Width, config.ReferenceRegion.Height)
	if err != nil {
		return nil, fmt.Errorf("resize OCR cascade reference image: %w", err)
	}
	resizeMS := float64(time.Since(resizeStarted).Microseconds()) / 1000
	capturedAt := region.Foreground.ObservedAt.UTC()
	identity := capturedAt.Format("20060102T150405.000000000Z")

	recognize := func(route ocraction.RouteConfig) (cascadeAttempt, error) {
		image, cropErr := reference.Crop(route)
		if cropErr != nil {
			return cascadeAttempt{}, fmt.Errorf("prepare OCR cascade route %s: %w", route.ID, cropErr)
		}
		result, recognizeErr := e.ocr.Recognize(ctx, action.RuleID, action.RuntimeProfile, ocrworker.Request{
			RequestID:  "ocr-action-" + identity + "-" + strings.ToLower(route.ID),
			ArtifactID: "screen-region-" + identity + "-" + strings.ToLower(route.ID),
			CapturedAt: capturedAt, Width: image.Width, Height: image.Height, RGB: image.RGB,
			CharacterConstraint: ocrworker.CharacterConstraint(config.CharacterConstraint),
		})
		if recognizeErr != nil {
			return cascadeAttempt{}, fmt.Errorf("run resident OCR cascade route %s with profile %s: %w", route.ID, action.RuntimeProfile, recognizeErr)
		}
		decision, accepted, state, decisionErr := e.runOCRCascadeDecision(ctx, action, *cascade, result)
		if decisionErr != nil {
			return cascadeAttempt{}, fmt.Errorf("decide OCR cascade route %s: %w", route.ID, decisionErr)
		}
		return cascadeAttempt{RouteID: route.ID, Image: image, Result: result, Decision: decision, Accepted: accepted, State: state}, nil
	}

	primary, err := recognize(cascade.Primary)
	if err != nil {
		return nil, err
	}
	attempts := []cascadeAttempt{primary}
	selected := primary
	finalState := primary.State
	terminalReason := "primary-accepted"
	var gate *ocraction.GateEvidence
	gateMS := 0.0
	transitions := []map[string]any{}
	if !primary.Accepted {
		gateStarted := time.Now()
		gateEvidence, gateErr := ocraction.EvaluateGate(native, cascade.Gate)
		gateMS = float64(time.Since(gateStarted).Microseconds()) / 1000
		if gateErr != nil {
			return nil, fmt.Errorf("evaluate OCR cascade gate: %w", gateErr)
		}
		gate = &gateEvidence
		finalState = cascade.UnknownState
		terminalReason = "primary-unknown-and-cheap-gate-rejected"
		if gateEvidence.Accepted {
			transitions = append(transitions, map[string]any{"from": cascade.Primary.ID, "to": cascade.Recovery.ID, "reason": "primary-unknown-and-cheap-gate-accepted"})
			recovery, recoveryErr := recognize(cascade.Recovery)
			if recoveryErr != nil {
				return nil, recoveryErr
			}
			attempts = append(attempts, recovery)
			allowed := false
			for _, state := range cascade.RecoveryAllowedStates {
				if recovery.State == state {
					allowed = true
					break
				}
			}
			switch {
			case !recovery.Accepted:
				terminalReason = "recovery-unknown"
			case !allowed:
				terminalReason = "recovery-state-not-eligible"
			case recovery.State != cascade.Validator.TriggerState:
				selected = recovery
				finalState = recovery.State
				terminalReason = "eligible-recovery-state-accepted"
			default:
				transitions = append(transitions, map[string]any{"from": cascade.Recovery.ID, "to": cascade.Validator.Route.ID, "reason": "recovery-state-requires-validator-agreement"})
				validator, validatorErr := recognize(cascade.Validator.Route)
				if validatorErr != nil {
					return nil, validatorErr
				}
				attempts = append(attempts, validator)
				if validator.Accepted && validator.State == cascade.Validator.RequiredState {
					selected = recovery
					finalState = recovery.State
					terminalReason = "validator-agreement"
				} else {
					terminalReason = "validator-disagreement"
				}
			}
		}
	}

	attemptOutputs := make([]map[string]any, 0, len(attempts))
	ocrTotalMS := 0.0
	for _, attempt := range attempts {
		ocrTotalMS += attempt.Result.Timing.TotalMS
		attemptOutputs = append(attemptOutputs, map[string]any{
			"routeId": attempt.RouteID, "text": attempt.Result.Text, "confidence": attempt.Result.Confidence,
			"decision": attempt.Decision,
			"evidence": map[string]any{
				"artifactId": attempt.Result.Evidence.ArtifactID, "capturedAt": attempt.Result.Evidence.CapturedAt,
				"rgbSha256": attempt.Result.Evidence.RGBSHA256,
				"image":     map[string]any{"width": attempt.Image.Width, "height": attempt.Image.Height, "encoding": "rgb24"},
			},
			"decoding": attempt.Result.Decoding, "model": attempt.Result.Model, "timing": attempt.Result.Timing,
		})
	}
	nativeDigest := sha256.Sum256(native.RGB)
	response := map[string]any{
		"schemaVersion": 2,
		"text":          selected.Result.Text, "confidence": selected.Result.Confidence,
		"decoding": selected.Result.Decoding,
		"evidence": map[string]any{
			"artifactId": selected.Result.Evidence.ArtifactID, "capturedAt": selected.Result.Evidence.CapturedAt,
			"rgbSha256":       selected.Result.Evidence.RGBSHA256,
			"sourceRgbSha256": fmt.Sprintf("%x", nativeDigest),
			"frame":           map[string]any{"width": region.FrameWidth, "height": region.FrameHeight, "foreground": map[string]any{"processId": region.Foreground.ProcessID, "executableName": region.Foreground.ExecutableName}},
			"coordinateSpace": map[string]any{"width": capture.ReferenceWidth, "height": capture.ReferenceHeight, "fit": "centered-16:9"},
			"referenceRegion": config.ReferenceRegion, "physicalRegion": region.PhysicalRegion,
			"sourceImage": map[string]any{"width": native.Width, "height": native.Height, "sampling": "native", "encoding": "rgb24"},
		},
		"model": selected.Result.Model,
		"timing": map[string]any{
			"captureMs": captureMS, "referenceResizeMs": resizeMS, "gateMs": gateMS,
			"ocrTotalMs": ocrTotalMS, "totalMs": float64(time.Since(started).Microseconds()) / 1000,
		},
		"cascade": map[string]any{
			"policy": "EXPLICIT_PERFORMANCE_FIRST", "attempts": attemptOutputs, "attemptCount": len(attemptOutputs),
			"gate": gate, "transitions": transitions, "selectedRoute": selected.RouteID,
			"finalState": finalState, "terminalReason": terminalReason, "selectedDecision": selected.Decision,
		},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode OCR cascade result: %w", err)
	}
	return encoded, nil
}

func (e *Executor) runOCRCascadeDecision(
	ctx context.Context,
	parent rules.Action,
	config ocraction.CascadeConfig,
	result ocrworker.Result,
) (json.RawMessage, bool, string, error) {
	decisionAction, err := e.rules.ResolveAction(config.DecisionActionID)
	if err != nil {
		return nil, false, "", fmt.Errorf("resolve decision Action %q: %w", config.DecisionActionID, err)
	}
	if decisionAction.RuleID != parent.RuleID || decisionAction.Runtime != rules.PureDecisionRuntimeV1 ||
		decisionAction.Exposure != rules.ActionExposureInternal || decisionAction.Execution.Completion != rules.CompletionReturn {
		return nil, false, "", errors.New("OCR cascade decision Action must be an internal same-Rule finite pure decision Action")
	}
	decisionPackage, err := scriptpackage.Load(decisionAction.Root, decisionAction.ID)
	if err != nil {
		return nil, false, "", fmt.Errorf("load OCR cascade decision Action: %w", err)
	}
	permissions := decisionPackage.Manifest.Permissions
	if permissions.Memory != nil || permissions.File != nil || permissions.Screen != nil || len(decisionPackage.Manifest.NativeLibraries) != 0 {
		return nil, false, "", errors.New("OCR cascade decision Action must not declare permissions or native libraries")
	}
	inputs := map[string]any{
		"schemaVersion": 1, "text": result.Text, "confidence": result.Confidence,
		"decoding": map[string]any{
			"characterConstraint": result.Decoding.CharacterConstraint,
			"rawText":             result.Decoding.RawText, "rawConfidence": result.Decoding.RawConfidence,
			"constrainedText": result.Decoding.ConstrainedText, "constrainedConfidence": result.Decoding.ConstrainedConfidence,
			"rawConstraintMargin": result.Decoding.RawConstraintMargin,
		},
		"evidence": map[string]any{
			"artifactId": result.Evidence.ArtifactID, "capturedAt": result.Evidence.CapturedAt,
			"width": result.Evidence.Width, "height": result.Evidence.Height, "rgbSha256": result.Evidence.RGBSHA256,
		},
		"model": map[string]any{
			"artifactId": result.Model.ArtifactID, "provider": result.Model.Provider,
			"adapterIndex": result.Model.AdapterIndex, "inputWidth": result.Model.InputWidth, "inputHeight": result.Model.InputHeight,
		},
		"timing": map[string]any{
			"preprocessMs": result.Timing.PreprocessMS, "inferenceMs": result.Timing.InferenceMS,
			"postprocessMs": result.Timing.PostprocessMS, "totalMs": result.Timing.TotalMS,
		},
	}
	decisionOutput, err := puredecision.Run(ctx, decisionPackage, inputs)
	if err != nil {
		return nil, false, "", fmt.Errorf("run decision Action %q: %w", config.DecisionActionID, err)
	}
	var document map[string]any
	if err := json.Unmarshal(decisionOutput, &document); err != nil {
		return nil, false, "", fmt.Errorf("decode decision Action output: %w", err)
	}
	acceptedValue, err := dottedObjectValue(document, config.DecisionAcceptedPath)
	if err != nil {
		return nil, false, "", err
	}
	accepted, ok := acceptedValue.(bool)
	if !ok {
		return nil, false, "", fmt.Errorf("decision path %q must resolve to a boolean", config.DecisionAcceptedPath)
	}
	stateValue, err := dottedObjectValue(document, config.DecisionStatePath)
	if err != nil {
		return nil, false, "", err
	}
	state, ok := stateValue.(string)
	if !ok || state == "" {
		return nil, false, "", fmt.Errorf("decision path %q must resolve to a non-empty string", config.DecisionStatePath)
	}
	if accepted == (state == config.UnknownState) {
		return nil, false, "", errors.New("OCR cascade decision accepted flag is inconsistent with the declared unknownState")
	}
	return append(json.RawMessage(nil), decisionOutput...), accepted, state, nil
}

func dottedObjectValue(document map[string]any, path string) (any, error) {
	var current any = document
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("decision path %q crosses a non-object at %q", path, part)
		}
		current, ok = object[part]
		if !ok {
			return nil, fmt.Errorf("decision path %q is missing %q", path, part)
		}
	}
	return current, nil
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
