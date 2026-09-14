package delegatedtask

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/piweb"
)

type fakeRuntime struct {
	events     chan piweb.Event
	errors     chan error
	prompts    []string
	behaviors  []string
	aborts     int
	endOnAbort bool
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{events: make(chan piweb.Event, 8), errors: make(chan error, 1)}
}

func (runtime *fakeRuntime) StartSession(context.Context, string) (piweb.Session, error) {
	return piweb.Session{ID: "session-1", CWD: `C:\work`}, nil
}
func (runtime *fakeRuntime) Subscribe(context.Context, piweb.Session) (piweb.EventStream, error) {
	return piweb.EventStream{Events: runtime.events, Errors: runtime.errors}, nil
}
func (runtime *fakeRuntime) Prompt(_ context.Context, _ piweb.Session, text, behavior string) error {
	runtime.prompts = append(runtime.prompts, text)
	runtime.behaviors = append(runtime.behaviors, behavior)
	return nil
}
func (runtime *fakeRuntime) Abort(context.Context, piweb.Session) error {
	runtime.aborts++
	if runtime.endOnAbort {
		runtime.events <- piweb.Event{Type: "agent.end", Seq: 1, Raw: []byte(`{"type":"agent.end","seq":1}`)}
	}
	return nil
}
func (runtime *fakeRuntime) WebURL(piweb.Session) string {
	return "http://127.0.0.1:8504/?session=session-1&view=chat"
}

func TestManagerCompletesAndReplaysNormalizedEvents(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := newFakeRuntime()
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "do work", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateRunning || task.SessionID != "session-1" || len(runtime.prompts) != 1 {
		t.Fatalf("task=%+v prompts=%v", task, runtime.prompts)
	}
	runtime.events <- piweb.Event{Type: "tool.start", Seq: 1, Raw: []byte(`{"type":"tool.start","seq":1,"toolName":"computer","toolCallId":"1","summary":"inspect"}`)}
	runtime.events <- piweb.Event{Type: "agent.end", Seq: 2, Raw: []byte(`{"type":"agent.end","seq":2}`)}
	runtime.events <- piweb.Event{Type: "status.update", Seq: 3, Raw: []byte(`{"type":"status.update","seq":3,"status":{"isStreaming":false,"isCompacting":false,"isBashRunning":false,"pendingMessageCount":0}}`)}
	waitForState(t, manager, task.TaskID, StateCompleted)
	events, next, last, err := manager.Events(context.Background(), task.TaskID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[0].Type != "task.created" || events[2].Type != "task.progress" || events[3].Type != "task.progress" || events[4].Type != "task.completed" || next != last {
		t.Fatalf("events=%+v next=%d last=%d", events, next, last)
	}
}

func TestFollowUpKeepsTaskRunningAcrossIntermediateAgentEnd(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := newFakeRuntime()
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "first", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Send(context.Background(), task.TaskID, MessageRequest{Text: "then second", Mode: "followUp"}); err != nil {
		t.Fatal(err)
	}
	runtime.events <- piweb.Event{Type: "agent.end", Seq: 1, Raw: []byte(`{"type":"agent.end","seq":1}`)}
	runtime.events <- piweb.Event{Type: "status.update", Seq: 2, Raw: []byte(`{"type":"status.update","seq":2,"status":{"isStreaming":false,"isCompacting":false,"isBashRunning":false,"pendingMessageCount":1}}`)}
	runtime.events <- piweb.Event{Type: "agent.start", Seq: 3, Raw: []byte(`{"type":"agent.start","seq":3}`)}
	waitForSequence(t, manager, task.TaskID, 6)
	current, err := manager.Get(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != StateRunning {
		t.Fatalf("intermediate state = %s", current.State)
	}
	runtime.events <- piweb.Event{Type: "agent.end", Seq: 4, Raw: []byte(`{"type":"agent.end","seq":4}`)}
	runtime.events <- piweb.Event{Type: "status.update", Seq: 5, Raw: []byte(`{"type":"status.update","seq":5,"status":{"isStreaming":false,"isCompacting":false,"isBashRunning":false,"pendingMessageCount":0}}`)}
	waitForState(t, manager, task.TaskID, StateCompleted)
}

func TestAttentionEventsKeepPiInteractionSchemaPrivate(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := newFakeRuntime()
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "work", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	runtime.events <- piweb.Event{Type: "ask.opened", Seq: 1, Raw: []byte(`{"type":"ask.opened","seq":1,"ask":{"askId":"ask-1","questions":[{"id":"secret-question","question":"private text","options":[]}]}}`)}
	runtime.events <- piweb.Event{Type: "dialog.opened", Seq: 2, Raw: []byte(`{"type":"dialog.opened","seq":2,"dialog":{"dialogId":"dialog-1","kind":"confirm","title":"private title"}}`)}
	waitForSequence(t, manager, task.TaskID, 4)
	current, err := manager.Get(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != StateWaitingInput {
		t.Fatalf("state = %s", current.State)
	}
	runtime.events <- piweb.Event{Type: "ask.closed", Seq: 3, Raw: []byte(`{"type":"ask.closed","seq":3,"askId":"ask-1","reason":"submitted"}`)}
	waitForSequence(t, manager, task.TaskID, 5)
	current, _ = manager.Get(task.TaskID)
	if current.State != StateWaitingInput {
		t.Fatalf("state after first resolution = %s", current.State)
	}
	runtime.events <- piweb.Event{Type: "dialog.closed", Seq: 4, Raw: []byte(`{"type":"dialog.closed","seq":4,"dialogId":"dialog-1","reason":"answered"}`)}
	waitForState(t, manager, task.TaskID, StateRunning)
	runtime.events <- piweb.Event{Type: "dialog.closed", Seq: 5, Raw: []byte(`{"type":"dialog.closed","seq":5,"dialogId":"dialog-1","answer":"private duplicate answer"}`)}
	waitForSequence(t, manager, task.TaskID, 7)
	events, _, _, err := manager.Events(context.Background(), task.TaskID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 || events[2].Type != "task.attention.required" || events[3].Type != "task.attention.required" || events[4].Type != "task.attention.resolved" || events[5].Type != "task.attention.resolved" || events[6].Type != "task.attention.resolved" {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events[2:] {
		payload := string(event.Payload)
		if strings.Contains(payload, "private text") || strings.Contains(payload, "private title") || strings.Contains(payload, "private duplicate answer") || strings.Contains(payload, "secret-question") {
			t.Fatalf("attention payload leaked PI interaction schema: %s", payload)
		}
	}
}

func TestMalformedAttentionEventFailsExplicitly(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := newFakeRuntime()
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "work", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	runtime.events <- piweb.Event{Type: "ask.opened", Seq: 1, Raw: []byte(`{"type":"ask.opened","seq":1,"ask":{"questions":[]}}`)}
	waitForState(t, manager, task.TaskID, StateFailed)
	failed, err := manager.Get(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.ErrorCode != "PI_WEB_EVENT_INVALID" || !strings.Contains(failed.Error, "missing askId") {
		t.Fatalf("failed task = %+v", failed)
	}
}

func TestManagerSteerCancelAndRestartRecovery(t *testing.T) {
	root := t.TempDir()
	store, err := eventstream.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakeRuntime()
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "do work", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Send(context.Background(), task.TaskID, MessageRequest{Text: "focus here", Mode: "steer"}); err != nil {
		t.Fatal(err)
	}
	if runtime.behaviors[1] != "steer" {
		t.Fatalf("behaviors = %v", runtime.behaviors)
	}
	if _, err := manager.Cancel(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	if runtime.aborts != 1 {
		t.Fatalf("aborts = %d", runtime.aborts)
	}
	manager.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := eventstream.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := NewManager(reopened, newFakeRuntime())
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	result, err := recovered.Get(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateFailed || result.ErrorCode != "ABORTED_BY_RUNTIME_RESTART" {
		t.Fatalf("recovered = %+v", result)
	}
}

func TestManagerRejectsUnknownTask(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewManager(store, newFakeRuntime())
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if _, err := manager.Get("missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestCancellationEventPrecedesCancelledTerminal(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := newFakeRuntime()
	runtime.endOnAbort = true
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.random = strings.NewReader("0123456789abcdef")
	task, err := manager.Submit(context.Background(), SubmitRequest{Prompt: "do work", CWD: `C:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Cancel(context.Background(), task.TaskID); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, task.TaskID, StateCancelled)
	events, _, _, err := manager.Events(context.Background(), task.TaskID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Type != "task.cancellation.requested" || events[3].Type != "task.cancelled" {
		t.Fatalf("events = %+v", events)
	}
}

func waitForState(t *testing.T, manager *Manager, taskID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task did not reach %s", want)
}

func waitForSequence(t *testing.T, manager *Manager, taskID string, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.LastSequence >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task did not reach sequence %d", want)
}
