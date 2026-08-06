package scenereducer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

type State struct {
	SchemaVersion         uint32         `json:"schemaVersion"`
	ModuleID              string         `json:"moduleId"`
	InputStream           string         `json:"inputStream"`
	Cursor                uint64         `json:"cursor"`
	LastOutputSequence    uint64         `json:"lastOutputSequence"`
	LastSummarySequence   uint64         `json:"lastSummarySequence"`
	LastSummaryObservedAt *time.Time     `json:"lastSummaryObservedAt,omitempty"`
	Scene                 *SceneState    `json:"scene,omitempty"`
	Baseline              *SceneState    `json:"baseline,omitempty"`
	Pending               *PendingOutput `json:"pending,omitempty"`
}

type SceneState struct {
	Signature       string         `json:"signature"`
	Tokens          []string       `json:"tokens"`
	ClassCounts     map[string]int `json:"classCounts"`
	DetectionCount  int            `json:"detectionCount"`
	FrameSHA256     string         `json:"frameSha256"`
	LastSequence    uint64         `json:"lastSequence"`
	LastObservedAt  time.Time      `json:"lastObservedAt"`
	FramesSinceEmit int            `json:"framesSinceEmit"`
}

type PendingOutput struct {
	InputSequence uint64                    `json:"inputSequence"`
	InputEventID  string                    `json:"inputEventId"`
	Request       eventstream.AppendRequest `json:"request"`
	Next          Checkpoint                `json:"next"`
}

type Checkpoint struct {
	Cursor                uint64      `json:"cursor"`
	LastSummarySequence   uint64      `json:"lastSummarySequence"`
	LastSummaryObservedAt *time.Time  `json:"lastSummaryObservedAt,omitempty"`
	Scene                 *SceneState `json:"scene,omitempty"`
	Baseline              *SceneState `json:"baseline,omitempty"`
}

type ParsedPayload struct {
	Tick             uint64          `json:"tick"`
	TargetExecutable string          `json:"targetExecutable"`
	Model            json.RawMessage `json:"model"`
	Frame            Frame           `json:"frame"`
	Inference        Inference       `json:"inference"`
	DetectionCount   int             `json:"detectionCount"`
	Detections       []Detection     `json:"detections"`
}

type Frame struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	RGBSHA256 string `json:"rgbSha256"`
}

type Inference struct {
	DurationMS   float64 `json:"durationMs"`
	Provider     string  `json:"provider"`
	AdapterIndex int     `json:"adapterIndex"`
	Device       string  `json:"device"`
	InputWidth   int     `json:"inputWidth"`
	InputHeight  int     `json:"inputHeight"`
	Confidence   float64 `json:"confidence"`
	IOU          float64 `json:"iou"`
}

type Detection struct {
	ClassID        int     `json:"classId"`
	Label          string  `json:"label"`
	Confidence     float64 `json:"confidence"`
	BBoxPixels     Box     `json:"bboxPixels"`
	BBoxNormalized Box     `json:"bboxNormalized"`
}

type Box struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

type LifecyclePayload struct {
	State            string `json:"state"`
	TargetExecutable string `json:"targetExecutable"`
	Activation       uint64 `json:"activation"`
	ProcessID        uint32 `json:"processId"`
	ArtifactID       string `json:"artifactId"`
}

type FailurePayload struct {
	Error    string `json:"error"`
	Terminal bool   `json:"terminal"`
	Tick     uint64 `json:"tick"`
}

type Reduction struct {
	Next    Checkpoint
	Request *eventstream.AppendRequest
}

func InitialState(config Config) State {
	return State{SchemaVersion: 1, ModuleID: config.ModuleID, InputStream: config.Input.Stream}
}

func (s State) Validate(config Config) error {
	if s.SchemaVersion != 1 || s.ModuleID != config.ModuleID || s.InputStream != config.Input.Stream {
		return errors.New("reducer state identity does not match config")
	}
	if s.LastSummarySequence > s.Cursor {
		return errors.New("reducer state lastSummarySequence is ahead of cursor")
	}
	if s.Scene != nil && s.Baseline == nil {
		return errors.New("reducer state with a current scene requires an emitted baseline")
	}
	if s.Pending != nil && s.Pending.InputSequence != s.Cursor+1 {
		return errors.New("reducer pending input sequence must immediately follow cursor")
	}
	return nil
}

func Reduce(config Config, state State, event eventstream.Event) (Reduction, error) {
	if err := state.Validate(config); err != nil {
		return Reduction{}, err
	}
	if state.Pending != nil {
		return Reduction{}, errors.New("cannot reduce while an output is pending recovery")
	}
	if event.Sequence != state.Cursor+1 {
		return Reduction{}, fmt.Errorf("event sequence gap: cursor=%d received=%d", state.Cursor, event.Sequence)
	}
	next := Checkpoint{
		Cursor:                event.Sequence,
		LastSummarySequence:   state.LastSummarySequence,
		LastSummaryObservedAt: cloneTime(state.LastSummaryObservedAt),
		Scene:                 cloneScene(state.Scene),
		Baseline:              cloneScene(state.Baseline),
	}
	if event.Stream != config.Input.Stream {
		return Reduction{Next: next}, nil
	}
	if event.Source.ModuleID != config.Input.ModuleID {
		return Reduction{}, fmt.Errorf("input stream source module mismatch: got %q", event.Source.ModuleID)
	}

	var outputType string
	var payload any
	switch event.Type {
	case config.Input.ParsedType:
		parsed, err := decodeParsed(event.Payload)
		if err != nil {
			return Reduction{}, err
		}
		if parsed.TargetExecutable != config.TargetExecutable || event.Foreground.ExecutableName != config.TargetExecutable {
			return Reduction{}, errors.New("parsed event target or foreground executable does not match reducer config")
		}
		current, regions, err := summarize(config, parsed, event)
		if err != nil {
			return Reduction{}, err
		}
		framesObserved := 1
		reason := "initial"
		changeScore := 1.0
		emit := state.Scene == nil
		stable := false
		if state.Scene != nil {
			if !event.ObservedAt.After(state.Scene.LastObservedAt) {
				return Reduction{}, errors.New("parsed event observedAt must increase")
			}
			framesObserved = state.Scene.FramesSinceEmit + 1
			changeScore = jaccardChange(state.Baseline.Tokens, current.Tokens)
			reason = "threshold"
			emit = changeScore >= config.Reducer.ChangeThreshold
			if !emit && state.LastSummaryObservedAt != nil && event.ObservedAt.Sub(*state.LastSummaryObservedAt) >= config.StableInterval() {
				emit = true
				stable = true
				reason = "interval"
			}
		}
		current.FramesSinceEmit = framesObserved
		next.Scene = current
		if !emit {
			return Reduction{Next: next}, nil
		}
		if stable {
			outputType = config.Output.SceneStableType
		} else {
			outputType = config.Output.SceneChangedType
		}
		payload = map[string]any{
			"schemaVersion": 2,
			"reason":        reason,
			"reducer": map[string]any{
				"runtime": RuntimeID, "baseline": "last-emitted", "positionQuantum": config.Reducer.PositionQuantum,
				"changeThreshold": config.Reducer.ChangeThreshold, "stableIntervalMs": config.Reducer.StableIntervalMS,
				"maxRegions": config.Reducer.MaxRegions,
			},
			"inputRange":     map[string]any{"after": state.LastSummarySequence, "through": event.Sequence, "framesObserved": framesObserved},
			"changeScore":    round(changeScore, 6),
			"sceneSignature": current.Signature,
			"frame":          parsed.Frame,
			"inference":      map[string]any{"durationMs": parsed.Inference.DurationMS, "provider": parsed.Inference.Provider},
			"detectionCount": parsed.DetectionCount,
			"classCounts":    current.ClassCounts,
			"classDelta":     classDelta(classCounts(state.Baseline), current.ClassCounts),
			"regions":        regions,
		}
		current.FramesSinceEmit = 0
		next.Scene = current
		next.Baseline = cloneScene(current)
		next.LastSummarySequence = event.Sequence
		next.LastSummaryObservedAt = timePointer(event.ObservedAt)
	case config.Input.LifecycleType:
		var lifecycle LifecyclePayload
		if err := decodeStrictPayload(event.Payload, &lifecycle); err != nil {
			return Reduction{}, fmt.Errorf("decode lifecycle payload: %w", err)
		}
		if lifecycle.TargetExecutable != config.TargetExecutable || lifecycle.State != "active" && lifecycle.State != "paused" {
			return Reduction{}, errors.New("lifecycle payload target or state is invalid")
		}
		next.Scene = nil
		next.Baseline = nil
		next.LastSummarySequence = event.Sequence
		next.LastSummaryObservedAt = timePointer(event.ObservedAt)
		outputType = config.Output.ForegroundChangedType
		payload = map[string]any{
			"schemaVersion": 1, "state": lifecycle.State, "targetExecutable": lifecycle.TargetExecutable,
			"foregroundExecutable": event.Foreground.ExecutableName, "foregroundRevision": event.Foreground.Revision,
			"activation": lifecycle.Activation, "processId": lifecycle.ProcessID, "modelArtifactId": lifecycle.ArtifactID,
		}
	case config.Input.FailureType:
		var failure FailurePayload
		if err := decodeStrictPayload(event.Payload, &failure); err != nil {
			return Reduction{}, fmt.Errorf("decode failure payload: %w", err)
		}
		if !failure.Terminal || strings.TrimSpace(failure.Error) == "" {
			return Reduction{}, errors.New("screenparser failure must be terminal and contain an error")
		}
		next.Scene = nil
		next.Baseline = nil
		next.LastSummarySequence = event.Sequence
		next.LastSummaryObservedAt = timePointer(event.ObservedAt)
		outputType = config.Output.SourceFailureType
		payload = map[string]any{"schemaVersion": 1, "error": failure.Error, "terminal": true, "tick": failure.Tick}
	default:
		return Reduction{}, fmt.Errorf("unsupported event type %q on configured input stream", event.Type)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Reduction{}, fmt.Errorf("encode reducer payload: %w", err)
	}
	request := eventstream.AppendRequest{
		SessionID: reducerSessionID(event.SessionID), Stream: config.Output.Stream, Type: outputType,
		ObservedAt: event.ObservedAt, Source: eventstream.Source{ModuleID: config.ModuleID, InstanceID: "screen-scene-reducer", Runtime: RuntimeID},
		Foreground: event.Foreground, CorrelationID: event.SessionID, CausationID: event.EventID, Payload: payloadJSON,
	}
	return Reduction{Next: next, Request: &request}, nil
}

func decodeParsed(data []byte) (ParsedPayload, error) {
	var parsed ParsedPayload
	if err := decodeStrictPayload(data, &parsed); err != nil {
		return ParsedPayload{}, fmt.Errorf("decode parsed payload: %w", err)
	}
	if parsed.Tick == 0 || parsed.Frame.Width <= 0 || parsed.Frame.Height <= 0 || len(parsed.Frame.RGBSHA256) != 64 || parsed.DetectionCount != len(parsed.Detections) {
		return ParsedPayload{}, errors.New("parsed payload frame, tick, or detection count is invalid")
	}
	if strings.TrimSpace(parsed.Inference.Provider) == "" || parsed.Inference.DurationMS < 0 {
		return ParsedPayload{}, errors.New("parsed payload inference is invalid")
	}
	return parsed, nil
}

func decodeStrictPayload(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("payload contains trailing JSON")
	}
	return nil
}

func summarize(config Config, parsed ParsedPayload, event eventstream.Event) (*SceneState, []map[string]any, error) {
	tokens := make([]string, 0, len(parsed.Detections))
	counts := make(map[string]int)
	ordered := append([]Detection(nil), parsed.Detections...)
	for _, detection := range parsed.Detections {
		if strings.TrimSpace(detection.Label) == "" || detection.Confidence < 0 || detection.Confidence > 1 {
			return nil, nil, errors.New("detection label or confidence is invalid")
		}
		box := detection.BBoxNormalized
		if box.Left < 0 || box.Top < 0 || box.Right > 1 || box.Bottom > 1 || box.Left >= box.Right || box.Top >= box.Bottom {
			return nil, nil, errors.New("detection normalized bounding box is invalid")
		}
		counts[detection.Label]++
		tokens = append(tokens, fmt.Sprintf("%s:%d:%d:%d:%d", detection.Label,
			quantize(box.Left, config.Reducer.PositionQuantum), quantize(box.Top, config.Reducer.PositionQuantum),
			quantize(box.Right, config.Reducer.PositionQuantum), quantize(box.Bottom, config.Reducer.PositionQuantum)))
	}
	sort.Strings(tokens)
	hash := sha256.Sum256([]byte(strings.Join(tokens, "\n")))
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Confidence == ordered[j].Confidence {
			return ordered[i].Label < ordered[j].Label
		}
		return ordered[i].Confidence > ordered[j].Confidence
	})
	limit := min(config.Reducer.MaxRegions, len(ordered))
	regions := make([]map[string]any, 0, limit)
	for _, detection := range ordered[:limit] {
		regions = append(regions, map[string]any{
			"label": detection.Label, "confidence": round(detection.Confidence, 4),
			"bboxNormalized": Box{Left: round(detection.BBoxNormalized.Left, 4), Top: round(detection.BBoxNormalized.Top, 4), Right: round(detection.BBoxNormalized.Right, 4), Bottom: round(detection.BBoxNormalized.Bottom, 4)},
		})
	}
	return &SceneState{Signature: hex.EncodeToString(hash[:]), Tokens: tokens, ClassCounts: counts,
		DetectionCount: parsed.DetectionCount, FrameSHA256: parsed.Frame.RGBSHA256, LastSequence: event.Sequence, LastObservedAt: event.ObservedAt}, regions, nil
}

func jaccardChange(previous, current []string) float64 {
	left, right, intersection, union := 0, 0, 0, 0
	for left < len(previous) || right < len(current) {
		switch {
		case left == len(previous):
			union++
			right++
		case right == len(current):
			union++
			left++
		case previous[left] == current[right]:
			intersection++
			union++
			left++
			right++
		case previous[left] < current[right]:
			union++
			left++
		default:
			union++
			right++
		}
	}
	if union == 0 {
		return 0
	}
	return 1 - float64(intersection)/float64(union)
}

func classCounts(scene *SceneState) map[string]int {
	if scene == nil {
		return map[string]int{}
	}
	return scene.ClassCounts
}

func classDelta(previous, current map[string]int) map[string]int {
	result := make(map[string]int)
	for label, value := range current {
		result[label] = value - previous[label]
	}
	for label, value := range previous {
		if _, ok := current[label]; !ok {
			result[label] = -value
		}
	}
	for label, value := range result {
		if value == 0 {
			delete(result, label)
		}
	}
	return result
}

func quantize(value, quantum float64) int { return int(math.Round(value / quantum)) }
func round(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}
func timePointer(value time.Time) *time.Time { copy := value; return &copy }
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePointer(*value)
}
func cloneScene(value *SceneState) *SceneState {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Tokens = append([]string(nil), value.Tokens...)
	copy.ClassCounts = make(map[string]int, len(value.ClassCounts))
	for key, count := range value.ClassCounts {
		copy.ClassCounts[key] = count
	}
	return &copy
}
func reducerSessionID(input string) string {
	sum := sha256.Sum256([]byte(input))
	return "scene-" + hex.EncodeToString(sum[:12])
}
