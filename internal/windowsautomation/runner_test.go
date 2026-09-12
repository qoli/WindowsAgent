package windowsautomation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type recordingProcessHost struct {
	mu      sync.Mutex
	request ProcessRequest
	result  ProcessResult
	err     error
	started chan struct{}
}

func (h *recordingProcessHost) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	h.mu.Lock()
	h.request = request
	h.mu.Unlock()
	if h.started != nil {
		select {
		case <-h.started:
		default:
			close(h.started)
		}
	}
	if h.err != nil {
		return ProcessResult{}, h.err
	}
	if h.result.ExitCode == -99 {
		<-ctx.Done()
		return ProcessResult{}, ctx.Err()
	}
	return h.result, nil
}

type recordingReporter struct {
	events []reportedEvent
}

type reportedEvent struct {
	kind    string
	payload json.RawMessage
}

func (r *recordingReporter) Emit(_ context.Context, kind string, payload json.RawMessage) (eventstream.Event, error) {
	r.events = append(r.events, reportedEvent{kind: kind, payload: append(json.RawMessage(nil), payload...)})
	return eventstream.Event{Sequence: uint64(len(r.events))}, nil
}

func TestRunnerPreservesArgvAndTreatsNonzeroExitAsData(t *testing.T) {
	argv := []any{"", "雪", `a"b`, `C:\dir\`}
	process := &recordingProcessHost{result: ProcessResult{ExitCode: 7, Stdout: "out", Stderr: "err", PID: 42, DurationMS: 9}}
	runner, err := NewRunner(process, OSFileHost{})
	if err != nil {
		t.Fatal(err)
	}
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    result = windows.process.run(
        executable=ctx.inputs["executable"],
        argv=ctx.inputs["argv"],
        cwd=ctx.inputs["cwd"],
        env={"MODE": "test"},
        stdin="payload",
    )
    return result
`, `{"type":"object","additionalProperties":true}`, `{"type":"object","required":["exitCode","stdout","stderr","pid","durationMs"]}`, 1<<20))
	output, err := runner.Run(context.Background(), pkg, map[string]any{"executable": "tool.exe", "argv": argv, "cwd": `C:\work`}, &recordingReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"exitCode":7`) {
		t.Fatalf("output = %s", output)
	}
	process.mu.Lock()
	request := process.request
	process.mu.Unlock()
	want := []string{"", "雪", `a"b`, `C:\dir\`}
	if !reflect.DeepEqual(request.Argv, want) || request.Executable != "tool.exe" || request.Cwd != `C:\work` || request.Stdin != "payload" || request.Env["MODE"] != "test" {
		t.Fatalf("request = %+v", request)
	}
	if request.MaxOutputBytes != pkg.Manifest.Limits.MaxProcessOutputBytes {
		t.Fatalf("output limit = %d", request.MaxOutputBytes)
	}
}

func TestOSProcessHostPreservesArgv(t *testing.T) {
	want := []string{"", "雪", `a"b`, `C:\dir\`}
	arguments := append([]string{"-test.run=TestProcessArgvHelper", "--"}, want...)
	result, err := (OSProcessHost{}).Run(context.Background(), ProcessRequest{
		Executable: os.Args[0], Argv: arguments, Env: map[string]string{"WINDOWSAUTOMATION_ARGV_HELPER": "1"}, MaxOutputBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
	}
	var got []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &got); err != nil {
		t.Fatalf("stdout=%q: %v", result.Stdout, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv=%q want=%q", got, want)
	}
}

func TestProcessArgvHelper(t *testing.T) {
	if os.Getenv("WINDOWSAUTOMATION_ARGV_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	if separator == 0 {
		os.Exit(3)
	}
	if err := json.NewEncoder(os.Stdout).Encode(os.Args[separator:]); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestRunnerFilesystemPrimitivesAndActivity(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "nested", "dir")
	source := filepath.Join(directory, "source.txt")
	copyName := filepath.Join(directory, "copy.txt")
	moved := filepath.Join(directory, "moved.txt")
	script := `def main(ctx):
    windows.fs.mkdir(path=ctx.inputs["directory"], parents=True)
    windows.fs.write(path=ctx.inputs["source"], content="hello")
    before = windows.fs.stat(path=ctx.inputs["source"])
    windows.fs.copy(source=ctx.inputs["source"], destination=ctx.inputs["copy"], overwrite=False)
    windows.fs.move(source=ctx.inputs["copy"], destination=ctx.inputs["moved"], overwrite=False)
    entries = windows.fs.list(path=ctx.inputs["directory"])
    content = windows.fs.read(path=ctx.inputs["moved"])
    windows.fs.remove(path=ctx.inputs["moved"], recursive=False)
    sequence = task.activity(message="filesystem complete", level="info")
    return {"content": content, "size": before["size"], "entries": len(entries), "sequence": sequence, "elapsed": task.elapsed_milliseconds()}
`
	pkg := mustLoadPackage(t, writePackage(t, script, `{"type":"object","additionalProperties":{"type":"string"}}`, `{"type":"object","additionalProperties":true}`, 1<<20))
	runner, _ := NewRunner(&recordingProcessHost{}, OSFileHost{})
	reporter := &recordingReporter{}
	output, err := runner.Run(context.Background(), pkg, map[string]any{"directory": directory, "source": source, "copy": copyName, "moved": moved}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) == "" || len(reporter.events) != 1 || reporter.events[0].kind != ActivityEventType {
		t.Fatalf("output=%s events=%+v", output, reporter.events)
	}
	if _, err := os.Stat(moved); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moved file remains: %v", err)
	}
	if content, err := os.ReadFile(source); err != nil || string(content) != "hello" {
		t.Fatalf("source=%q err=%v", content, err)
	}
}

func TestRunnerReturnsTypedHostFailure(t *testing.T) {
	process := &recordingProcessHost{err: errors.New("spawn denied")}
	runner, _ := NewRunner(process, OSFileHost{})
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    return windows.process.run(executable="missing.exe", argv=[])
`, `{}`, `{}`, 1<<20))
	_, err := runner.Run(context.Background(), pkg, map[string]any{}, &recordingReporter{})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "PROCESS_RUN_FAILED" || typed.Stage != "windows.process.run" {
		t.Fatalf("error = %#v", err)
	}
}

func TestTaskFailReturnsCallerOwnedTypedFailure(t *testing.T) {
	runner, _ := NewRunner(&recordingProcessHost{}, OSFileHost{})
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    task.fail(code="REMOTE_POSTCONDITION_FAILED", message="application did not load the requested state")
`, `{}`, `{}`, 1<<20))
	_, err := runner.Run(context.Background(), pkg, map[string]any{}, &recordingReporter{})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "REMOTE_POSTCONDITION_FAILED" || typed.Stage != "task.fail" {
		t.Fatalf("error = %#v", err)
	}
}

func TestRunnerCancellationInterruptsHostAndStarlark(t *testing.T) {
	process := &recordingProcessHost{result: ProcessResult{ExitCode: -99}, started: make(chan struct{})}
	runner, _ := NewRunner(process, OSFileHost{})
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    return windows.process.run(executable="wait.exe", argv=[])
`, `{}`, `{}`, 1<<20))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, pkg, map[string]any{}, &recordingReporter{}); done <- err }()
	<-process.started
	cancel()
	select {
	case err := <-done:
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "ACTION_CANCELLED" {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not cancel")
	}
}

func TestRunnerEnforcesOutputSchemaAndResultBound(t *testing.T) {
	tests := []struct {
		name, schema, want string
		max                uint64
	}{
		{"schema", `{"type":"object","required":["missing"]}`, "AUTOMATION_OUTPUT_INVALID", 100},
		{"bound", `{}`, "AUTOMATION_RESULT_LIMIT_EXCEEDED", 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pkg := mustLoadPackage(t, writePackage(t, `def main(ctx): return {"value": "long"}`, `{}`, test.schema, test.max))
			runner, _ := NewRunner(&recordingProcessHost{}, OSFileHost{})
			_, err := runner.Run(context.Background(), pkg, map[string]any{}, &recordingReporter{})
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != test.want {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOSFileHostCopiesDirectoryAndFailsExplicitlyWithoutOverwrite(t *testing.T) {
	host := OSFileHost{}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "value.txt"), "value")
	if err := host.Copy(context.Background(), source, destination, false); err != nil {
		t.Fatal(err)
	}
	if err := host.Copy(context.Background(), source, destination, false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("error = %v", err)
	}
	if err := host.Remove(context.Background(), filepath.Join(root, "missing"), true); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing recursive remove error = %v", err)
	}
}

func mustLoadPackage(t *testing.T, root string) *Package {
	t.Helper()
	pkg, err := LoadDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}
