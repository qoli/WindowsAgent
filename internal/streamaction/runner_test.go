package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type fixtureCaller struct{ calls int }

func (f *fixtureCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	f.calls++
	if id != "game/status" || inputs["detail"] != true {
		return nil, errors.New("unexpected child Action call")
	}
	return json.RawMessage(`{"massLock":"OFF"}`), nil
}

type fixtureReporter struct {
	types    []string
	payloads []json.RawMessage
}

func (f *fixtureReporter) Emit(_ context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	f.types = append(f.types, eventType)
	f.payloads = append(f.payloads, append(json.RawMessage(nil), payload...))
	return eventstream.Event{Sequence: uint64(len(f.types))}, nil
}

func TestRunnerCallsFiniteActionEmitsValidatedEventAndReturnsOutput(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    status = action.call(id="game/status", inputs={"detail": True})
    sequence = stream.emit(type="action.phase.changed", payload={"phase": "DONE", "massLock": status["massLock"]})
    task.sleep(milliseconds=1)
    return {"done": True, "sequence": sequence}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	caller := &fixtureCaller{}
	reporter := &fixtureReporter{}
	output, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, caller, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"done":true,"sequence":1}` || caller.calls != 1 || len(reporter.types) != 1 || reporter.types[0] != "action.phase.changed" {
		t.Fatalf("output=%s calls=%d events=%v", output, caller.calls, reporter.types)
	}
}

func TestRunnerRejectsEventOutsidePackageSchema(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    stream.emit(type="action.invalid", payload={"phase": "DONE"})
    return {"done": True, "sequence": 1}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "event schema") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerSleepIsCancelled(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    task.sleep(milliseconds=5000)
    return {"done": True, "sequence": 0}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (Runner{}).Run(ctx, pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled streaming Action did not stop")
	}
}

func writeFixturePackage(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.star":          script,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  `{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"type":"boolean"}}}`,
		"output.schema.json": `{"type":"object","additionalProperties":false,"required":["done","sequence"],"properties":{"done":{"const":true},"sequence":{"type":"integer","minimum":0}}}`,
		"event.schema.json":  `{"type":"object","additionalProperties":false,"required":["type","payload"],"properties":{"type":{"const":"action.phase.changed"},"payload":{"type":"object","additionalProperties":false,"required":["phase","massLock"],"properties":{"phase":{"const":"DONE"},"massLock":{"const":"OFF"}}}}}`,
	}
	manifest := `{
  "schemaVersion":1,
  "version":1,
  "title":"Fixture streaming Action",
  "entrypoint":"main.star",
  "taskDocument":"TASK.md",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "eventSchema":"event.schema.json",
  "files":["main.star","TASK.md","input.schema.json","output.schema.json","event.schema.json"],
  "limits":{"maxSteps":100000,"maxOutputBytes":4096,"maxEventBytes":4096,"maxSleepMs":10000}
}`
	files[ManifestName] = manifest
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func contains(value, expected string) bool {
	for index := 0; index+len(expected) <= len(value); index++ {
		if value[index:index+len(expected)] == expected {
			return true
		}
	}
	return false
}
