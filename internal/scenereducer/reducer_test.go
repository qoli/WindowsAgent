package scenereducer

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

func TestRetiredPalworldReducerFixtureLoads(t *testing.T) {
	name := filepath.Join("testdata", "palworld-retired-manifest.json")
	config, err := LoadConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	if config.ModuleID != "screen/scene-reducer" || config.Input.Stream != "screenparser.palworld" || config.Output.Stream != "screen.semantic.palworld" {
		t.Fatalf("config = %+v", config)
	}
}

func TestReduceEmitsInitialThenStableSummary(t *testing.T) {
	config := testConfig()
	state := InitialState(config)
	first := parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{testDetection("Button", .9, .10, .10, .20, .20)})
	reduction, err := Reduce(config, state, first)
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Request == nil || reduction.Request.Type != config.Output.SceneChangedType || reduction.Request.CausationID != first.EventID {
		t.Fatalf("initial reduction = %+v", reduction)
	}
	state = applyReduction(state, reduction, 2)

	second := parsedEvent(t, 3, time.Unix(102, 0).UTC(), []Detection{testDetection("Button", .89, .101, .101, .201, .201)})
	reduction, err = Reduce(config, state, second)
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Request != nil {
		t.Fatalf("stable frame emitted too early: %+v", reduction.Request)
	}
	state = applyReduction(state, reduction, 0)

	third := parsedEvent(t, 4, time.Unix(131, 0).UTC(), []Detection{testDetection("Button", .88, .102, .102, .202, .202)})
	reduction, err = Reduce(config, state, third)
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Request == nil || reduction.Request.Type != config.Output.SceneStableType {
		t.Fatalf("interval reduction = %+v", reduction)
	}
	var payload map[string]any
	if err := json.Unmarshal(reduction.Request.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reason"] != "interval" || payload["changeScore"].(float64) != 0 {
		t.Fatalf("stable payload = %s", reduction.Request.Payload)
	}
	if payload["schemaVersion"].(float64) != 2 || payload["reducer"].(map[string]any)["baseline"] != "last-emitted" {
		t.Fatalf("stable payload provenance = %s", reduction.Request.Payload)
	}
}

func TestReduceEmitsMeaningfulLayoutChange(t *testing.T) {
	config := testConfig()
	state := InitialState(config)
	first, err := Reduce(config, state, parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{
		testDetection("Button", .9, .10, .10, .20, .20),
		testDetection("Text", .8, .30, .30, .40, .40),
	}))
	if err != nil {
		t.Fatal(err)
	}
	state = applyReduction(state, first, 2)
	changed, err := Reduce(config, state, parsedEvent(t, 3, time.Unix(102, 0).UTC(), []Detection{
		testDetection("Image", .95, .70, .70, .90, .90),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if changed.Request == nil || changed.Request.Type != config.Output.SceneChangedType {
		t.Fatalf("changed reduction = %+v", changed)
	}
}

func TestReduceComparesAgainstLastEmittedBaseline(t *testing.T) {
	config := testConfig()
	config.Reducer.ChangeThreshold = .55
	state := InitialState(config)
	initial, err := Reduce(config, state, parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{
		testDetection("Button", .9, .10, .10, .20, .20),
		testDetection("Text", .8, .30, .30, .40, .40),
		testDetection("Image", .7, .50, .50, .60, .60),
	}))
	if err != nil {
		t.Fatal(err)
	}
	state = applyReduction(state, initial, 2)
	second, err := Reduce(config, state, parsedEvent(t, 3, time.Unix(102, 0).UTC(), []Detection{
		testDetection("Link", .9, .70, .10, .80, .20),
		testDetection("Text", .8, .30, .30, .40, .40),
		testDetection("Image", .7, .50, .50, .60, .60),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if second.Request != nil {
		t.Fatal("one accumulated replacement must remain below threshold")
	}
	state = applyReduction(state, second, 0)
	third, err := Reduce(config, state, parsedEvent(t, 4, time.Unix(104, 0).UTC(), []Detection{
		testDetection("Link", .9, .70, .10, .80, .20),
		testDetection("Heading", .8, .70, .30, .80, .40),
		testDetection("Image", .7, .50, .50, .60, .60),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if third.Request == nil || third.Request.Type != config.Output.SceneChangedType {
		t.Fatalf("accumulated baseline change was not emitted: %+v", third)
	}
}

func TestReduceLifecycleResetsSceneAndRejectsUnknownInputType(t *testing.T) {
	config := testConfig()
	state := InitialState(config)
	initial, err := Reduce(config, state, parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{testDetection("Button", .9, .1, .1, .2, .2)}))
	if err != nil {
		t.Fatal(err)
	}
	state = applyReduction(state, initial, 2)
	lifecyclePayload, _ := json.Marshal(LifecyclePayload{State: "paused", TargetExecutable: config.TargetExecutable, Activation: 1, ProcessID: 99, ArtifactID: "model"})
	lifecycle := eventstream.Event{
		SchemaVersion: 1, Sequence: 3, EventID: "evt_lifecycle", SessionID: "input-session", Stream: config.Input.Stream,
		Type: config.Input.LifecycleType, ObservedAt: time.Unix(102, 0).UTC(), CommittedAt: time.Unix(102, 0).UTC(),
		Source:     eventstream.Source{ModuleID: config.Input.ModuleID, InstanceID: "source", Runtime: "screenparser"},
		Foreground: eventstream.Foreground{ExecutableName: "Notepad.exe", Revision: 2}, Payload: lifecyclePayload,
	}
	reduction, err := Reduce(config, state, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Request == nil || reduction.Request.Type != config.Output.ForegroundChangedType || reduction.Next.Scene != nil {
		t.Fatalf("lifecycle reduction = %+v", reduction)
	}
	state = applyReduction(state, reduction, 4)
	unknown := lifecycle
	unknown.Sequence = 5
	unknown.EventID = "evt_unknown"
	unknown.Type = "screenparser.unknown"
	if _, err := Reduce(config, state, unknown); err == nil || !strings.Contains(err.Error(), "unsupported event type") {
		t.Fatalf("unknown type error = %v", err)
	}
}

func TestReduceRejectsSequenceGapAndUnknownPayloadField(t *testing.T) {
	config := testConfig()
	state := InitialState(config)
	event := parsedEvent(t, 2, time.Unix(100, 0).UTC(), []Detection{testDetection("Button", .9, .1, .1, .2, .2)})
	if _, err := Reduce(config, state, event); err == nil || !strings.Contains(err.Error(), "sequence gap") {
		t.Fatalf("gap error = %v", err)
	}
	event.Sequence = 1
	event.Payload = append(event.Payload[:len(event.Payload)-1], []byte(`,"unknown":true}`)...)
	if _, err := Reduce(config, state, event); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func testConfig() Config {
	return Config{
		SchemaVersion: 1, ModuleID: "screen/scene-reducer", Kind: "reactor", Runtime: RuntimeID, TargetExecutable: "Game.exe",
		Input:   InputConfig{ModuleID: "screenparser/ui-elements", Stream: "screenparser.game", ParsedType: "screenparser.parsed", LifecycleType: "screenparser.lifecycle", FailureType: "screenparser.failure"},
		Output:  OutputConfig{Stream: "screen.semantic.game", SceneChangedType: "screen.scene.changed", SceneStableType: "screen.scene.stable", ForegroundChangedType: "screen.foreground.changed", SourceFailureType: "screen.source.failure"},
		Reducer: ReducerConfig{PositionQuantum: .02, ChangeThreshold: .15, StableIntervalMS: 30000, MaxRegions: 12},
	}
}

func parsedEvent(t *testing.T, sequence uint64, observedAt time.Time, detections []Detection) eventstream.Event {
	t.Helper()
	payload, err := json.Marshal(ParsedPayload{
		Tick: sequence, TargetExecutable: "Game.exe", Model: json.RawMessage(`{}`),
		Frame:          Frame{Width: 3840, Height: 2160, RGBSHA256: strings.Repeat("a", 64)},
		Inference:      Inference{DurationMS: 140, Provider: "DirectML", AdapterIndex: 0, Device: "directml:0", InputWidth: 1280, InputHeight: 1280, Confidence: .1, IOU: .1},
		DetectionCount: len(detections), Detections: detections,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventstream.Event{
		SchemaVersion: 1, Sequence: sequence, EventID: "evt_" + strings.Repeat("a", 20) + string(rune('a'+sequence)),
		SessionID: "input-session", Stream: "screenparser.game", Type: "screenparser.parsed", ObservedAt: observedAt, CommittedAt: observedAt,
		Source:     eventstream.Source{ModuleID: "screenparser/ui-elements", InstanceID: "source-instance", Runtime: "screenparser-onnx-dml-v1"},
		Foreground: eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1}, Payload: payload,
	}
}

func testDetection(label string, confidence, left, top, right, bottom float64) Detection {
	return Detection{ClassID: 1, Label: label, Confidence: confidence, BBoxPixels: Box{Left: 1, Top: 1, Right: 2, Bottom: 2}, BBoxNormalized: Box{Left: left, Top: top, Right: right, Bottom: bottom}}
}

func applyReduction(state State, reduction Reduction, outputSequence uint64) State {
	state.Cursor = reduction.Next.Cursor
	state.Scene = reduction.Next.Scene
	state.Baseline = reduction.Next.Baseline
	state.LastSummarySequence = reduction.Next.LastSummarySequence
	state.LastSummaryObservedAt = reduction.Next.LastSummaryObservedAt
	if outputSequence != 0 {
		state.LastOutputSequence = outputSequence
		state.Cursor = outputSequence
	}
	return state
}
