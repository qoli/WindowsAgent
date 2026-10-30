package windowsautomation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

type recordingInputHost struct {
	keys    []string
	clicks  []pointeraction.DirectClickRequest
	clicked chan struct{}
	err     error
}

func (host *recordingInputHost) PressDirect(_ context.Context, request inputaction.DirectPressRequest) (inputaction.DirectPressResult, error) {
	host.keys = append(host.keys, request.Key)
	return inputaction.DirectPressResult{SchemaVersion: 1, Key: request.Key}, host.err
}

func (host *recordingInputHost) ClickDirect(_ context.Context, request pointeraction.DirectClickRequest) (pointeraction.DirectClickResult, error) {
	host.clicks = append(host.clicks, request)
	if host.clicked != nil {
		close(host.clicked)
	}
	return pointeraction.DirectClickResult{SchemaVersion: 1, Operation: request.Operation, ScreenX: *request.X, ScreenY: *request.Y}, host.err
}

func inputFixture() map[string]any {
	return map[string]any{"foreground": map[string]any{"processId": 42, "executableName": "Fixture.exe", "executablePath": `C:\Apps\Fixture.exe`}}
}

func TestEphemeralInputWorkflowNeedsNoRuleAndReportsEachResult(t *testing.T) {
	host := &recordingInputHost{}
	runner, _ := NewInteractiveRunner(&recordingProcessHost{}, OSFileHost{}, host, host)
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    key = windows.input.press(expected_foreground=ctx.inputs["foreground"], key="Key_Home", hold_ms=40)
    clicks = []
    for x in [100, 200]:
        clicks.append(windows.pointer.click(expected_foreground=ctx.inputs["foreground"], x=x, y=300, hold_ms=40))
        task.activity(message="click completed", level="info")
        task.sleep(milliseconds=1)
    return {"key": key, "clicks": clicks}
`, `{}`, `{}`, 1<<20))
	reporter := &recordingReporter{}
	output, err := runner.Run(context.Background(), pkg, inputFixture(), reporter)
	if err != nil || len(host.keys) != 1 || len(host.clicks) != 2 || *host.clicks[1].X != 200 || len(reporter.events) != 2 {
		t.Fatalf("output=%s keys=%v clicks=%+v events=%v err=%v", output, host.keys, host.clicks, reporter.events, err)
	}
	if !json.Valid(output) {
		t.Fatalf("output=%s", output)
	}
}

func TestCancellingSleepPreventsLaterPointerOperation(t *testing.T) {
	host := &recordingInputHost{clicked: make(chan struct{})}
	runner, _ := NewInteractiveRunner(&recordingProcessHost{}, OSFileHost{}, host, host)
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    windows.pointer.click(expected_foreground=ctx.inputs["foreground"], x=100, y=200, hold_ms=40)
    task.sleep(milliseconds=1000)
    windows.input.press(expected_foreground=ctx.inputs["foreground"], key="Key_Home", hold_ms=40)
    return {}
`, `{}`, `{}`, 1<<20))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := runner.Run(ctx, pkg, inputFixture(), &recordingReporter{}); done <- err }()
	select {
	case <-host.clicked:
	case <-time.After(2 * time.Second):
		t.Fatal("pointer was not called")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || len(host.keys) != 0 {
			t.Fatalf("keys=%v err=%v", host.keys, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sleep did not cancel")
	}
}

func TestInputFailurePreservesCodeAndDoesNotRunFollowingOperations(t *testing.T) {
	for _, code := range []string{"INPUT_FOREGROUND_CHANGED", "INPUT_RELEASE_FAILED"} {
		t.Run(code, func(t *testing.T) {
			host := &recordingInputHost{err: &windowsinput.Error{Code: code, Stage: "injecting-input", Cause: errors.New("fixture failure")}}
			runner, _ := NewInteractiveRunner(&recordingProcessHost{}, OSFileHost{}, host, host)
			pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    windows.input.press(expected_foreground=ctx.inputs["foreground"], key="Key_Home", hold_ms=40)
    windows.pointer.click(expected_foreground=ctx.inputs["foreground"], x=100, y=200, hold_ms=40)
    return {}
`, `{}`, `{}`, 1<<20))
			_, err := runner.Run(context.Background(), pkg, inputFixture(), &recordingReporter{})
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != code || len(host.clicks) != 0 {
				t.Fatalf("error=%v clicks=%v", err, host.clicks)
			}
		})
	}
}

func TestMissingInputHostFailsExplicitly(t *testing.T) {
	runner, _ := NewLocalRunner()
	pkg := mustLoadPackage(t, writePackage(t, `def main(ctx):
    return windows.input.press(expected_foreground=ctx.inputs["foreground"], key="Key_Home", hold_ms=40)
`, `{}`, `{}`, 1<<20))
	_, err := runner.Run(context.Background(), pkg, inputFixture(), &recordingReporter{})
	if err == nil || !strings.Contains(err.Error(), "AUTOMATION_INPUT_UNAVAILABLE") {
		t.Fatalf("error=%v", err)
	}
}

func TestShippedPointerCyclePackageLoadsAndRunsWithoutRule(t *testing.T) {
	pkg, err := LoadDirectory(filepath.Join("..", "..", "docs", "examples", "windows-starlark-pointer-cycle"))
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingInputHost{}
	runner, _ := NewInteractiveRunner(&recordingProcessHost{}, OSFileHost{}, host, host)
	inputs := inputFixture()
	inputs["first"], inputs["second"], inputs["pauseMs"] = map[string]any{"x": 100, "y": 200}, map[string]any{"x": 200, "y": 200}, 1
	output, err := runner.Run(context.Background(), pkg, inputs, &recordingReporter{})
	if err != nil || len(host.clicks) != 2 {
		t.Fatalf("output=%s err=%v", output, err)
	}
}
