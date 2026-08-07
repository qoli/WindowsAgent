package actionlaunch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/ocrworker"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
)

type fakeObservationExecutor struct{}

func (fakeObservationExecutor) Run(context.Context, scriptlaunch.Invocation) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true,"output":{"ready":true}}`), nil
}

type fakeRegionCapturer struct{ result capture.RegionResult }

func (f fakeRegionCapturer) CaptureRegion(context.Context, capture.RegionRequest) (capture.RegionResult, error) {
	return f.result, nil
}

type fakeStreamingReporter struct{ types []string }

func (f *fakeStreamingReporter) Emit(_ context.Context, eventType string, _ json.RawMessage) (eventstream.Event, error) {
	f.types = append(f.types, eventType)
	return eventstream.Event{Sequence: uint64(len(f.types))}, nil
}

func TestStreamingActionCallsSameRuleFiniteChild(t *testing.T) {
	rulesRoot := t.TempDir()
	ruleRoot := filepath.Join(rulesRoot, "game.exe")
	workflowRoot := filepath.Join(ruleRoot, "Actions", "workflow")
	childRoot := filepath.Join(ruleRoot, "Actions", "status")
	if err := os.MkdirAll(workflowRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(childRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(ruleRoot, rules.RuleFilename), `{
  "schemaVersion":5,
  "description":"Streaming fixture.",
  "runtimeProfiles":{},
  "actions":{
    "game/workflow":{"path":"Actions/workflow","runtime":"windows-streaming-action-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":true},"registrableAs":[]},
    "game/status":{"path":"Actions/status","runtime":"windows-observation-v1","execution":{"completion":"return"},"registrableAs":[]}
  },
  "registrations":{}
}`)
	writeTestFile(t, filepath.Join(ruleRoot, rules.AgentsFilename), "# Fixture\n")
	writeStreamingPackage(t, workflowRoot)
	ruleStore, err := rules.New(rulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := New(
		ruleStore, fakeObservationExecutor{}, fakeRegionCapturer{}, &fakeOCRRecognizer{},
		func() (foreground.Info, error) { return foreground.Info{ExecutableName: "game.exe"}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeStreamingReporter{}
	result, err := executor.RunStreaming(context.Background(), scriptlaunch.Invocation{
		Capability: "game/workflow", Inputs: map[string]any{"enabled": true},
	}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Output) != `{"done":true}` || len(reporter.types) != 1 || reporter.types[0] != "action.child.ready" {
		t.Fatalf("result=%s events=%v", result.Output, reporter.types)
	}
}

func writeStreamingPackage(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"main.star": `def main(ctx):
    child = action.call(id="game/status", inputs={})
    stream.emit(type="action.child.ready", payload={"ready": child["ready"]})
    return {"done": True}
`,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  `{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"type":"boolean"}}}`,
		"output.schema.json": `{"type":"object","additionalProperties":false,"required":["done"],"properties":{"done":{"const":true}}}`,
		"event.schema.json":  `{"type":"object","additionalProperties":false,"required":["type","payload"],"properties":{"type":{"const":"action.child.ready"},"payload":{"type":"object","additionalProperties":false,"required":["ready"],"properties":{"ready":{"const":true}}}}}`,
		"manifest.json":      `{"schemaVersion":1,"version":1,"title":"Fixture","entrypoint":"main.star","taskDocument":"TASK.md","inputSchema":"input.schema.json","outputSchema":"output.schema.json","eventSchema":"event.schema.json","files":["main.star","TASK.md","input.schema.json","output.schema.json","event.schema.json"],"limits":{"maxSteps":10000,"maxOutputBytes":4096,"maxEventBytes":4096,"maxSleepMs":1000}}`,
	}
	for name, content := range files {
		writeTestFile(t, filepath.Join(root, name), content)
	}
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type fakeOCRRecognizer struct {
	request   ocrworker.Request
	ruleID    string
	profileID string
}

func (f *fakeOCRRecognizer) Recognize(_ context.Context, ruleID, profileID string, request ocrworker.Request) (ocrworker.Result, error) {
	f.request, f.ruleID, f.profileID = request, ruleID, profileID
	return ocrworker.Result{
		RequestID: request.RequestID, CompletedAt: request.CapturedAt.Add(time.Millisecond),
		Text: "ALIGN WITH TARGET DESTINATION", Confidence: .998932,
		Evidence: ocrworker.Evidence{
			ArtifactID: request.ArtifactID, CapturedAt: request.CapturedAt,
			Width: request.Width, Height: request.Height,
			RGBSHA256: "c41fb7d586569efaf21768e8e34c70e16adfcd7f8b937368f4fa252874efb06f",
		},
		Model: ocrworker.Model{
			ArtifactID: "ppocrv6-small-rec-onnx-official-w480", Provider: "DirectML",
			AdapterIndex: 0, InputWidth: 480, InputHeight: 48,
		},
		Timing: ocrworker.Timing{InferenceMS: 80, TotalMS: 100},
	}, nil
}

func TestOCRActionReturnsRawTextEvidence(t *testing.T) {
	rulesRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	ruleStore, err := rules.New(rulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Date(2026, 8, 7, 1, 5, 12, 388601900, time.UTC)
	foregroundInfo := foreground.Info{
		ObservedAt: observedAt, ProcessID: 42,
		ExecutableName: "EliteDangerous64.exe", ExecutablePath: `C:\Games\EliteDangerous64.exe`,
	}
	pixels := make([]uint32, 400*40)
	for index := range pixels {
		pixels[index] = 0x010203
	}
	recognizer := &fakeOCRRecognizer{}
	executor, err := New(
		ruleStore,
		fakeObservationExecutor{},
		fakeRegionCapturer{result: capture.RegionResult{
			Pixels: pixels, ImageWidth: 400, ImageHeight: 40,
			FrameWidth: 3840, FrameHeight: 2160,
			PhysicalRegion: capture.PixelRegion{Left: 1520, Top: 720, Width: 800, Height: 80},
			Foreground:     foregroundInfo,
		}},
		recognizer,
		func() (foreground.Info, error) { return foregroundInfo, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := executor.Run(context.Background(), scriptlaunch.Invocation{
		Capability: "elite-dangerous/flight-prompt-text", Inputs: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ActionID string `json:"actionId"`
		Output   struct {
			Text       string  `json:"text"`
			Confidence float64 `json:"confidence"`
		} `json:"output"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if response.ActionID != "elite-dangerous/flight-prompt-text" || response.Output.Text != "ALIGN WITH TARGET DESTINATION" || response.Output.Confidence != .998932 {
		t.Fatalf("response = %s", encoded)
	}
	if recognizer.ruleID != "EliteDangerous64.exe" || recognizer.profileID != "ocr/w480" ||
		recognizer.request.Width != 400 || recognizer.request.Height != 40 || len(recognizer.request.RGB) != 400*40*3 {
		t.Fatalf("OCR request dimensions = %dx%d, RGB bytes = %d", recognizer.request.Width, recognizer.request.Height, len(recognizer.request.RGB))
	}
	if recognizer.request.RGB[0] != 1 || recognizer.request.RGB[1] != 2 || recognizer.request.RGB[2] != 3 {
		t.Fatal("packed pixels were not converted to RGB24")
	}
}
