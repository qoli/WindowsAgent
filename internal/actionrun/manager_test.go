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
	"github.com/qoli/WindowsAgent/internal/actionsequence"
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

func (j storeJournal) Replay(ctx context.Context, after uint64, limit int) ([]eventstream.Event, uint64, uint64, error) {
	events, err := j.store.ReadAfter(ctx, after, limit)
	if err != nil {
		return nil, 0, 0, err
	}
	last, err := j.store.LastSequence()
	if err != nil {
		return nil, 0, 0, err
	}
	next := after
	if len(events) != 0 {
		next = events[len(events)-1].Sequence
	}
	return events, next, last, nil
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
	started          chan struct{}
	release          chan struct{}
	once             sync.Once
	output           json.RawMessage
	err              error
	emit             bool
	panic            any
	mu               sync.Mutex
	calls            []string
	validationErrors map[string]error
}

func (f *fakeExecutor) ValidateAction(invocation scriptlaunch.Invocation) (rules.Action, error) {
	if err := f.validationErrors[invocation.Capability]; err != nil {
		return rules.Action{}, err
	}
	switch invocation.Capability {
	case "game/finite":
		return rules.Action{ID: invocation.Capability, RuleID: "Game.exe", Runtime: rules.WindowsKeyActionRuntimeV1, Execution: rules.ActionExecution{Completion: rules.CompletionReturn}, SequenceEligible: true}, nil
	case "game/linear":
		return rules.Action{ID: invocation.Capability, RuleID: "Game.exe", Runtime: rules.StreamingActionRuntimeV1, Execution: rules.ActionExecution{Completion: rules.CompletionStream, Lifecycle: rules.LifecycleLinear, Interruptible: true}, SequenceEligible: true}, nil
	default:
		return rules.Action{}, errors.New("unknown fixture Action")
	}
}

func (f *fakeExecutor) Contract(actionID string) (actionlaunch.Contract, error) {
	action, err := f.ValidateAction(scriptlaunch.Invocation{Capability: actionID, Inputs: map[string]any{}})
	if err != nil {
		return actionlaunch.Contract{}, err
	}
	return actionlaunch.Contract{Action: action, Title: actionID, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":true}`)}, nil
}

func (f *fakeExecutor) RunAction(_ context.Context, invocation scriptlaunch.Invocation) (actionlaunch.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, invocation.Capability)
	f.mu.Unlock()
	return actionlaunch.Result{ActionID: invocation.Capability, RuleID: "Game.exe", Runtime: "fixture-v1", Output: json.RawMessage(`{"value":1}`)}, nil
}

func (f *fakeExecutor) RunStreaming(ctx context.Context, invocation scriptlaunch.Invocation, reporter streamaction.Reporter) (actionlaunch.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, invocation.Capability)
	f.mu.Unlock()
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

func (f *fakeExecutor) callSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
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

func TestManagerTerminalizesInterruptedInvocationDuringStartup(t *testing.T) {
	executor := &fakeExecutor{}
	previous, store := newTestManager(t, executor)
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	started, err := store.Append(context.Background(), eventstream.AppendRequest{
		SessionID: "act_interrupted", Stream: StreamName, Type: "action.started",
		ObservedAt:    time.Now().UTC(),
		Source:        eventstream.Source{ModuleID: "game/linear", InstanceID: "act_interrupted", Runtime: rules.StreamingActionRuntimeV1},
		Foreground:    eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1},
		CorrelationID: "act_interrupted",
		Payload:       json.RawMessage(`{"state":"RUNNING","actionId":"game/linear","lifecycle":"linear","interruptible":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(previous.rules, executor, storeJournal{store: store}, previous.foreground, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })

	status, err := manager.Get("act_interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateFailed || !strings.Contains(status.Error, "process exited") || status.Stop != nil {
		t.Fatalf("status = %+v", status)
	}
	events, err := store.ReadAfter(context.Background(), started.Sequence-1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(eventTypes(events), ","); got != "action.started,action.failed" {
		t.Fatalf("event types = %s", got)
	}
	if !strings.Contains(string(events[1].Payload), `"errorCode":"ABORTED_BY_AGENT_RESTART"`) || events[1].CausationID != started.EventID {
		t.Fatalf("terminal event = %+v", events[1])
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(previous.rules, executor, storeJournal{store: store}, previous.foreground, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Close() })
	status, err = restarted.Get("act_interrupted")
	if err != nil || status.State != StateFailed || !strings.Contains(status.Error, "process exited") {
		t.Fatalf("status after second restart = %+v, err = %v", status, err)
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

func TestActionSequencePreflightsEveryStepBeforeFirstEffect(t *testing.T) {
	executor := &fakeExecutor{validationErrors: map[string]error{"game/linear": errors.New("invalid literal input")}}
	manager, _ := newTestManager(t, executor)
	_, err := manager.InvokeSequence(context.Background(), actionsequence.Request{
		RuleID: "Game.exe",
		Steps: []actionsequence.Step{
			{Action: "game/finite", Inputs: map[string]any{}},
			{Action: "game/linear", Inputs: map[string]any{"bad": true}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("preflight error = %v", err)
	}
	if calls := executor.callSnapshot(); len(calls) != 0 {
		t.Fatalf("preflight caused effects: %v", calls)
	}
}

func TestActionSequenceRunsFiniteStepsInOrderAndForwardsOutputs(t *testing.T) {
	executor := &fakeExecutor{}
	manager, _ := newTestManager(t, executor)
	response, err := manager.InvokeSequence(context.Background(), actionsequence.Request{
		RuleID: "Game.exe",
		Steps: []actionsequence.Step{
			{Action: "game/finite", Inputs: map[string]any{"order": 1}},
			{Action: "game/finite", Inputs: map[string]any{"order": 2}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := strings.Join(eventTypes(events), ","); got != "action.started,action.sequence.started,action.sequence.step.started,action.sequence.child.output,action.sequence.step.completed,action.sequence.step.started,action.sequence.child.output,action.sequence.step.completed,action.completed" {
		t.Fatalf("event types = %s", got)
	}
	if calls := strings.Join(executor.callSnapshot(), ","); calls != "game/finite,game/finite" {
		t.Fatalf("calls = %s", calls)
	}
}

func TestActionSequenceForwardsStreamingChildAndCanCancelIt(t *testing.T) {
	executor := &fakeExecutor{started: make(chan struct{}), release: make(chan struct{}), emit: true}
	manager, _ := newTestManager(t, executor)
	response, err := manager.InvokeSequence(context.Background(), actionsequence.Request{
		RuleID: "Game.exe", Steps: []actionsequence.Step{{Action: "game/linear", Inputs: map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	if _, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/finite", Inputs: map[string]any{}}); !errors.Is(err, ErrRuleSequenceActive) {
		t.Fatalf("external Action conflict error = %v", err)
	}
	if _, err := manager.Stop(response.InvocationID); err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := strings.Join(eventTypes(events), ","); !strings.Contains(got, "action.sequence.child.event") || !strings.HasSuffix(got, "action.cancelled") {
		t.Fatalf("event types = %s", got)
	}
	manager.wg.Wait()
	status, err := manager.Get(response.InvocationID)
	if err != nil || status.State != StateCancelled {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
	instance, err := manager.lookup(response.InvocationID)
	if err != nil || instance.sequence != nil {
		t.Fatalf("terminal sequence AST was retained: instance = %+v, err = %v", instance, err)
	}
	if _, err := manager.Invoke(context.Background(), scriptlaunch.Invocation{Capability: "game/finite", Inputs: map[string]any{}}); err != nil {
		t.Fatalf("Rule lease was not released: %v", err)
	}
}

func TestActionSequenceForwardsStreamingChildThroughNaturalCompletion(t *testing.T) {
	executor := &fakeExecutor{emit: true, output: json.RawMessage(`{"done":true}`)}
	manager, _ := newTestManager(t, executor)
	response, err := manager.InvokeSequence(context.Background(), actionsequence.Request{
		RuleID: "Game.exe", Steps: []actionsequence.Step{{Action: "game/linear", Inputs: map[string]any{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectUntilTerminal(t, manager, response.InvocationID, response.Watch.AfterCursor)
	if got := strings.Join(eventTypes(events), ","); got != "action.started,action.sequence.started,action.sequence.step.started,action.sequence.child.event,action.sequence.child.output,action.sequence.step.completed,action.completed" {
		t.Fatalf("event types = %s", got)
	}
	for _, event := range events {
		if event.CorrelationID != response.InvocationID || event.Source.ModuleID != actionsequence.ActionID {
			t.Fatalf("event escaped parent chain: %+v", event)
		}
	}
	var started map[string]any
	if err := json.Unmarshal(events[0].Payload, &started); err != nil {
		t.Fatal(err)
	}
	if _, exists := started["stepCount"]; exists {
		t.Fatalf("generic action.started contains Sequence-only metadata: %s", events[0].Payload)
	}
	var sequenceStarted actionsequence.StartedEvent
	if err := json.Unmarshal(events[1].Payload, &sequenceStarted); err != nil || sequenceStarted.StepCount != 1 {
		t.Fatalf("sequence started = %+v, err = %v", sequenceStarted, err)
	}
	var child actionsequence.ChildEvent
	if err := json.Unmarshal(events[3].Payload, &child); err != nil {
		t.Fatal(err)
	}
	if child.Type != "action.phase.changed" || child.ActionID != "game/linear" || child.ChildExecutionID == "" || string(child.Payload) != `{"phase":"WAITING"}` {
		t.Fatalf("child event = %+v", child)
	}
	manager.wg.Wait()
	manager.mu.Lock()
	runCount := len(manager.runs)
	manager.mu.Unlock()
	if runCount != 1 {
		t.Fatalf("Sequence created addressable child invocations: run count = %d", runCount)
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
  "schemaVersion":6,
  "description":"Fixture.",
  "runtimeProfiles":{},
  "actions":{
    "game/finite":{"path":"Actions/finite","runtime":"windows-key-action-v1","execution":{"completion":"return"},"registrableAs":[]},
    "game/linear":{"path":"Actions/linear","runtime":"windows-streaming-action-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":true},"registrableAs":[]},
    "game/fixed-linear":{"path":"Actions/fixed-linear","runtime":"fixture-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":false},"registrableAs":[]},
    "game/loop":{"path":"Actions/loop","runtime":"fixture-v1","execution":{"completion":"stream","lifecycle":"loop","interruptible":true},"registrableAs":[]}
  },
  "ephemeralActionSequence":{"allowedActions":["game/finite","game/linear"]},
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
	manager.random = strings.NewReader(strings.Repeat("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", 10))
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
