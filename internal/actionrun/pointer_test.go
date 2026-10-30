package actionrun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/pointeraction"
)

func TestDirectPointerRunsOutsideRuleAndReplaysTerminalResult(t *testing.T) {
	manager, _ := newTestManager(t, &fakeExecutor{})
	request := pointeraction.DirectClickRequest{SchemaVersion: 1, Operation: pointeraction.OperationClickCurrentPosition,
		ExpectedForeground: testDirectKeyRequest().ExpectedForeground, HoldMS: 40}
	response, err := manager.InvokeDirectPointer(context.Background(), request)
	if err != nil || response.RuleID != "" || response.ActionID != DirectPointerInputActionID || response.Watch == nil || response.Stop == nil {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := strings.Join(eventTypes(events), ","); got != "action.started,action.completed" {
		t.Fatalf("events=%s", got)
	}
	status, err := manager.Get(response.InvocationID)
	if err != nil || status.State != StateCompleted || !strings.Contains(string(status.Output), `"button":"LEFT"`) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestDirectPointerReleaseFailureOverridesCancellation(t *testing.T) {
	executor := &fakeExecutor{directStarted: make(chan struct{}), directRelease: make(chan struct{}),
		directErr: &inputaction.Error{Code: "INPUT_RELEASE_FAILED", Stage: "releasing-input", Cause: errors.New("release failed")}}
	manager, _ := newTestManager(t, executor)
	request := pointeraction.DirectClickRequest{SchemaVersion: 1, Operation: pointeraction.OperationClickCurrentPosition,
		ExpectedForeground: testDirectKeyRequest().ExpectedForeground, HoldMS: 40}
	response, err := manager.InvokeDirectPointer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	<-executor.directStarted
	if _, err := manager.Stop(response.InvocationID); err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if last := events[len(events)-1]; last.Type != "action.failed" || !strings.Contains(string(last.Payload), "INPUT_RELEASE_FAILED") {
		t.Fatalf("terminal=%+v", last)
	}
}
