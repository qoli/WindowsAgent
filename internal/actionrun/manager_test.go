package actionrun

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionlaunch"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/streamaction"
)

var errTerminalSeen = errors.New("terminal event seen")

type storeJournal struct{ store *eventstream.Store }

func (j storeJournal) Append(ctx context.Context, request eventstream.AppendRequest) (eventstream.Event, error) {
	return j.store.Append(ctx, request)
}

func (j storeJournal) Stream(ctx context.Context, after uint64, visit func(eventstream.Event) error) error {
	cursor := after
	for {
		events, err := j.store.WaitAfter(ctx, cursor, eventstream.DefaultReplayLimit)
		if err != nil {
			return err
		}
		for _, event := range events {
			cursor = event.Sequence
			if err := visit(event); err != nil {
				return err
			}
		}
	}
}

type fakeExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	output  json.RawMessage
	err     error
	emit    bool
	panic   any
}

func (f *fakeExecutor) RunAction(_ context.Context, invocation scriptlaunch.Invocation) (actionlaunch.Result, error) {
	return actionlaunch.Result{ActionID: invocation.Capability, RuleID: "Game.exe", Runtime: "fixture-v1", Output: json.RawMessage(`{"value":1}`)}, nil
}

func (f *fakeExecutor) RunStreaming(ctx context.Context, invocation scriptlaunch.Invocation, reporter streamaction.Reporter) (actionlaunch.Result, error) {
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.emit {
		if _, err := reporter.Emit(ctx, "action.phase.changed", json.RawMessage(`{"phase":"WAITING"}`)); err != nil {
			return actionlaunch.Result{}, err
		}
	}
	if f.panic != nil {
		panic(f.panic)
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return actionlaunch.Result{}, ctx.Err()
		}
	}
	return actionlaunch.Result{ActionID: invocation.Capability, RuleID: "Game.exe", Runtime: "fixture-v1", Output: f.output}, f.err
}

func TestStreamingActionPanicFailsInvocationWithoutCrashingManager(t *testing.T) {
	executor := &fakeExecutor{panic: "live Action fault"}
	manager, _ := newTestManager(t, executor)
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/linear", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := eventTypes(events); strings.Join(got, ",") != "action.started,action.failed" {
		t.Fatalf("event types = %v", got)
	}
	status, err := manager.Get(response.InvocationID)
	if err != nil || status.State != StateFailed || !strings.Contains(status.Error, "live Action fault") {
		t.Fatalf("status = %+v, err = %v", status, err)
	}

	executor.panic = nil
	executor.output = json.RawMessage(`{"done":true}`)
	manager.random = strings.NewReader("fedcba9876543210")
	next, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/linear", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	collectUntilTerminal(t, manager, next.InvocationID, next.Watch.AfterCursor)
	status, err = manager.Get(next.InvocationID)
	if err != nil || status.State != StateCompleted {
		t.Fatalf("manager did not survive panic: status = %+v, err = %v", status, err)
	}
}

func TestFiniteActionReturnsTerminalOutputWithoutEventCallback(t *testing.T) {
	manager, store := newTestManager(t, &fakeExecutor{})
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/finite", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if response.State != StateCompleted || response.Watch != nil || response.Stop != nil || string(response.Output) != `{"value":1}` {
		t.Fatalf("response = %+v", response)
	}
	last, err := store.LastSequence()
	if err != nil || last != 0 {
		t.Fatalf("event sequence = %d, err = %v", last, err)
	}
}

func TestLinearStreamingActionReturnsWatchThenCompletesItself(t *testing.T) {
	executor := &fakeExecutor{started: make(chan struct{}), release: make(chan struct{}), output: json.RawMessage(`{"leftStation":true}`), emit: true}
	manager, store := newTestManager(t, executor)
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/linear", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if response.State != StateRunning || response.Watch == nil || response.Stop == nil || response.Watch.AfterCursor != 0 {
		t.Fatalf("response = %+v", response)
	}
	<-executor.started
	close(executor.release)
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := eventTypes(events); strings.Join(got, ",") != "action.started,action.phase.changed,action.completed" {
		t.Fatalf("event types = %v", got)
	}
	status, err := manager.Get(response.InvocationID)
	if err != nil || status.State != StateCompleted {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
	last, _ := store.LastSequence()
	if last != 3 {
		t.Fatalf("last sequence = %d", last)
	}
}

func TestInterruptibleLoopStopsAndEmitsCancelled(t *testing.T) {
	executor := &fakeExecutor{started: make(chan struct{}), release: make(chan struct{})}
	manager, _ := newTestManager(t, executor)
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/loop", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	stopping, err := manager.Stop(response.InvocationID)
	if err != nil || stopping.State != StateCancelling {
		t.Fatalf("stop = %+v, err = %v", stopping, err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := eventTypes(events); strings.Join(got, ",") != "action.started,action.cancelled" {
		t.Fatalf("event types = %v", got)
	}
	again, err := manager.Stop(response.InvocationID)
	if err != nil || again.State != StateCancelled {
		t.Fatalf("repeated stop = %+v, err = %v", again, err)
	}
}

func TestLoopNaturalReturnFailsExplicitly(t *testing.T) {
	executor := &fakeExecutor{output: json.RawMessage(`{"unexpected":true}`)}
	manager, _ := newTestManager(t, executor)
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/loop", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := eventTypes(events); strings.Join(got, ",") != "action.started,action.failed" {
		t.Fatalf("event types = %v", got)
	}
	status, _ := manager.Get(response.InvocationID)
	if status.State != StateFailed || !strings.Contains(status.Error, "returned without cancellation") {
		t.Fatalf("status = %+v", status)
	}
}

func TestNonInterruptibleLinearActionOmitsStopAndRejectsCancellation(t *testing.T) {
	executor := &fakeExecutor{output: json.RawMessage(`{"done":true}`)}
	manager, _ := newTestManager(t, executor)
	response, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/fixed-linear", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Stop != nil {
		t.Fatalf("non-interruptible response has stop target: %+v", response.Stop)
	}
	if _, err := manager.Stop(response.InvocationID); !errors.Is(err, ErrNotInterruptible) {
		t.Fatalf("stop error = %v", err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := eventTypes(events); strings.Join(got, ",") != "action.started,action.completed" {
		t.Fatalf("event types = %v", got)
	}
}

func newTestManager(t *testing.T, executor *fakeExecutor) (*Manager, *eventstream.Store) {
	t.Helper()
	rulesRoot := t.TempDir()
	ruleRoot := filepath.Join(rulesRoot, "Game.exe")
	for _, name := range []string{"finite", "linear", "fixed-linear", "loop"} {
		if err := os.MkdirAll(filepath.Join(ruleRoot, "Actions", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	descriptor := `{
  "schemaVersion":5,
  "description":"Fixture.",
  "runtimeProfiles":{},
  "actions":{
    "game/finite":{"path":"Actions/finite","runtime":"fixture-v1","execution":{"completion":"return"},"registrableAs":[]},
    "game/linear":{"path":"Actions/linear","runtime":"fixture-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":true},"registrableAs":[]},
    "game/fixed-linear":{"path":"Actions/fixed-linear","runtime":"fixture-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":false},"registrableAs":[]},
    "game/loop":{"path":"Actions/loop","runtime":"fixture-v1","execution":{"completion":"stream","lifecycle":"loop","interruptible":true},"registrableAs":[]}
  },
  "registrations":{}
}`
	if err := os.WriteFile(filepath.Join(ruleRoot, rules.RuleFilename), []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleRoot, rules.AgentsFilename), []byte("# Fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ruleStore, err := rules.New(rulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager, err := NewManager(ruleStore, executor, storeJournal{store: store}, func() (foreground.Info, error) {
		return foreground.Info{
			ObservedAt: time.Now().UTC(), ProcessID: 42, ExecutableName: "Game.exe", ExecutablePath: `C:\Games\Game.exe`,
		}, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	manager.random = strings.NewReader("0123456789abcdef")
	t.Cleanup(func() { manager.Close() })
	return manager, store
}

func collectUntilTerminal(t *testing.T, manager *Manager, identity string, after uint64) []eventstream.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var events []eventstream.Event
	err := manager.Stream(ctx, identity, after, func(event eventstream.Event) error {
		events = append(events, event)
		if event.Type == "action.completed" || event.Type == "action.failed" || event.Type == "action.cancelled" {
			return errTerminalSeen
		}
		return nil
	})
	if !errors.Is(err, errTerminalSeen) {
		t.Fatalf("stream error = %v", err)
	}
	return events
}

func eventTypes(events []eventstream.Event) []string {
	result := make([]string, len(events))
	for index, event := range events {
		result[index] = event.Type
	}
	return result
}
