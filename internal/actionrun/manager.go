// Package actionrun owns the common finite and streaming Action invocation lifecycle.
package actionrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionlaunch"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/streamaction"
)

const StreamName = "action.runs"

var (
	ErrInvocationNotFound = errors.New("Action invocation not found")
	ErrNotInterruptible   = errors.New("streaming Action does not declare interruptible capability")
)

const (
	StateRunning    = "RUNNING"
	StateCancelling = "CANCELLING"
	StateCompleted  = "COMPLETED"
	StateFailed     = "FAILED"
	StateCancelled  = "CANCELLED"
)

type Journal interface {
	Append(context.Context, eventstream.AppendRequest) (eventstream.Event, error)
	Stream(context.Context, uint64, func(eventstream.Event) error) error
}

type Executor interface {
	RunAction(context.Context, scriptlaunch.Invocation) (actionlaunch.Result, error)
	RunStreaming(context.Context, scriptlaunch.Invocation, streamaction.Reporter) (actionlaunch.Result, error)
}

type WatchTarget struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	AfterCursor uint64 `json:"afterCursor"`
}

type StopTarget struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type Invocation struct {
	InvocationID string                `json:"invocationId"`
	ActionID     string                `json:"actionId"`
	RuleID       string                `json:"ruleId"`
	Runtime      string                `json:"runtime"`
	State        string                `json:"state"`
	Execution    rules.ActionExecution `json:"execution"`
	Output       json.RawMessage       `json:"output,omitempty"`
	Watch        *WatchTarget          `json:"watch,omitempty"`
	Stop         *StopTarget           `json:"stop,omitempty"`
	Error        string                `json:"error,omitempty"`
}

type Manager struct {
	rules      *rules.Store
	executor   Executor
	journal    Journal
	foreground func() (foreground.Info, error)
	now        func() time.Time
	random     io.Reader

	mu     sync.Mutex
	runs   map[string]*run
	closed bool
	wg     sync.WaitGroup
}

type run struct {
	manager    *Manager
	action     rules.Action
	invocation scriptlaunch.Invocation
	identity   string
	foreground foreground.Info
	ctx        context.Context
	cancel     context.CancelFunc

	mu          sync.Mutex
	eventMu     sync.Mutex
	state       string
	errorText   string
	lastEventID string
	afterCursor uint64
}

func NewManager(ruleStore *rules.Store, executor Executor, journal Journal, foregroundSnapshot func() (foreground.Info, error)) (*Manager, error) {
	if ruleStore == nil || executor == nil || journal == nil || foregroundSnapshot == nil {
		return nil, errors.New("Rule store, Action executor, event journal, and foreground resolver are required")
	}
	return &Manager{
		rules: ruleStore, executor: executor, journal: journal, foreground: foregroundSnapshot,
		now: time.Now, random: rand.Reader, runs: map[string]*run{},
	}, nil
}

func (m *Manager) Invoke(ctx context.Context, invocation scriptlaunch.Invocation) (Invocation, error) {
	if m == nil {
		return Invocation{}, errors.New("Action invocation manager is required")
	}
	if ctx == nil {
		return Invocation{}, errors.New("context is required")
	}
	if invocation.Capability == "" || strings.TrimSpace(invocation.Capability) != invocation.Capability || invocation.Inputs == nil {
		return Invocation{}, errors.New("canonical capability and inputs object are required")
	}
	action, err := m.rules.ResolveAction(invocation.Capability)
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve Action %q: %w", invocation.Capability, err)
	}
	identity, err := newInvocationID(m.random)
	if err != nil {
		return Invocation{}, fmt.Errorf("create Action invocation ID: %w", err)
	}
	if action.Execution.Completion == rules.CompletionReturn {
		result, err := m.executor.RunAction(ctx, invocation)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{
			InvocationID: identity, ActionID: result.ActionID, RuleID: result.RuleID,
			Runtime: result.Runtime, State: StateCompleted, Execution: action.Execution,
			Output: append(json.RawMessage(nil), result.Output...),
		}, nil
	}
	return m.startStreaming(action, invocation, identity)
}

func (m *Manager) startStreaming(action rules.Action, invocation scriptlaunch.Invocation, identity string) (Invocation, error) {
	observed, err := m.foreground()
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve foreground before streaming Action: %w", err)
	}
	if !strings.EqualFold(observed.ExecutableName, action.RuleID) {
		return Invocation{}, fmt.Errorf("foreground executable is %q, expected owning Rule %q", observed.ExecutableName, action.RuleID)
	}
	runContext, cancel := context.WithCancel(context.Background())
	instance := &run{
		manager: m, action: action, invocation: invocation, identity: identity,
		foreground: observed, ctx: runContext, cancel: cancel, state: StateRunning,
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return Invocation{}, errors.New("Action invocation manager is closed")
	}
	m.runs[identity] = instance
	m.mu.Unlock()
	started, err := instance.appendEvent(context.Background(), "action.started", map[string]any{
		"state": StateRunning, "actionId": action.ID, "lifecycle": action.Execution.Lifecycle,
		"interruptible": action.Execution.Interruptible,
	})
	if err != nil {
		cancel()
		m.mu.Lock()
		delete(m.runs, identity)
		m.mu.Unlock()
		return Invocation{}, fmt.Errorf("commit streaming Action start event: %w", err)
	}
	instance.mu.Lock()
	instance.afterCursor = started.Sequence - 1
	instance.mu.Unlock()
	m.wg.Add(1)
	go instance.execute()
	response := Invocation{
		InvocationID: identity, ActionID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime,
		State: StateRunning, Execution: action.Execution,
		Watch: &WatchTarget{
			URL:         "/v1/action-invocations/" + identity + "/events?after=" + fmt.Sprint(started.Sequence-1),
			ContentType: "application/x-ndjson", AfterCursor: started.Sequence - 1,
		},
	}
	if action.Execution.Interruptible {
		response.Stop = &StopTarget{Method: "POST", URL: "/v1/action-invocations/" + identity + "/stop"}
	}
	return response, nil
}

func (m *Manager) Stop(identity string) (Invocation, error) {
	instance, err := m.lookup(identity)
	if err != nil {
		return Invocation{}, err
	}
	if !instance.action.Execution.Interruptible {
		return Invocation{}, ErrNotInterruptible
	}
	instance.mu.Lock()
	switch instance.state {
	case StateRunning:
		instance.state = StateCancelling
		instance.cancel()
	case StateCancelling, StateCompleted, StateFailed, StateCancelled:
		// Repeated stop is idempotent for the exact invocation.
	default:
		instance.mu.Unlock()
		return Invocation{}, fmt.Errorf("streaming Action has invalid state %q", instance.state)
	}
	response := instance.snapshotLocked()
	instance.mu.Unlock()
	return response, nil
}

func (m *Manager) Get(identity string) (Invocation, error) {
	instance, err := m.lookup(identity)
	if err != nil {
		return Invocation{}, err
	}
	instance.mu.Lock()
	defer instance.mu.Unlock()
	return instance.snapshotLocked(), nil
}

func (m *Manager) Stream(ctx context.Context, identity string, after uint64, visit func(eventstream.Event) error) error {
	if _, err := m.lookup(identity); err != nil {
		return err
	}
	if visit == nil {
		return errors.New("event visitor is required")
	}
	return m.journal.Stream(ctx, after, func(event eventstream.Event) error {
		if event.CorrelationID != identity {
			return nil
		}
		return visit(event)
	})
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	for _, instance := range m.runs {
		instance.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

func (m *Manager) lookup(identity string) (*run, error) {
	if identity == "" || strings.TrimSpace(identity) != identity {
		return nil, errors.New("canonical invocation ID is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	instance := m.runs[identity]
	if instance == nil {
		return nil, ErrInvocationNotFound
	}
	return instance, nil
}

func (r *run) execute() {
	defer r.manager.wg.Done()
	result, runErr := r.manager.executor.RunStreaming(r.ctx, r.invocation, r)
	r.mu.Lock()
	cancelled := r.ctx.Err() != nil
	r.mu.Unlock()
	var state, eventType string
	var payload map[string]any
	switch {
	case cancelled:
		state, eventType = StateCancelled, "action.cancelled"
		payload = map[string]any{"state": state}
	case runErr != nil:
		state, eventType = StateFailed, "action.failed"
		payload = map[string]any{"state": state, "error": runErr.Error()}
	case r.action.Execution.Lifecycle == rules.LifecycleLoop:
		state, eventType = StateFailed, "action.failed"
		runErr = errors.New("loop streaming Action returned without cancellation")
		payload = map[string]any{"state": state, "error": runErr.Error()}
	case len(result.Output) == 0 || !json.Valid(result.Output):
		state, eventType = StateFailed, "action.failed"
		runErr = errors.New("linear streaming Action returned invalid output")
		payload = map[string]any{"state": state, "error": runErr.Error()}
	default:
		state, eventType = StateCompleted, "action.completed"
		payload = map[string]any{"state": state, "output": json.RawMessage(result.Output)}
	}
	_, appendErr := r.appendEvent(context.Background(), eventType, payload)
	r.mu.Lock()
	r.state = state
	if runErr != nil {
		r.errorText = runErr.Error()
	}
	if appendErr != nil {
		r.state = StateFailed
		r.errorText = "commit terminal Action event: " + appendErr.Error()
	}
	r.mu.Unlock()
}

func (r *run) Emit(ctx context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	if !strings.HasPrefix(eventType, "action.") || eventType == "action.started" || eventType == "action.completed" ||
		eventType == "action.failed" || eventType == "action.cancelled" {
		return eventstream.Event{}, errors.New("streaming Action event type must be a non-terminal action.* type")
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return eventstream.Event{}, errors.New("streaming Action event payload must be valid JSON")
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return eventstream.Event{}, err
	}
	return r.appendEvent(ctx, eventType, value)
}

func (r *run) appendEvent(ctx context.Context, eventType string, payload any) (eventstream.Event, error) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return eventstream.Event{}, err
	}
	r.mu.Lock()
	causationID := r.lastEventID
	r.mu.Unlock()
	event, err := r.manager.journal.Append(ctx, eventstream.AppendRequest{
		SessionID: r.identity, Stream: StreamName, Type: eventType,
		ObservedAt:    r.manager.now().UTC(),
		Source:        eventstream.Source{ModuleID: r.action.ID, InstanceID: r.identity, Runtime: r.action.Runtime},
		Foreground:    eventstream.Foreground{ExecutableName: r.action.RuleID, Revision: 1},
		CorrelationID: r.identity, CausationID: causationID, Payload: encoded,
	})
	if err != nil {
		return eventstream.Event{}, err
	}
	r.mu.Lock()
	r.lastEventID = event.EventID
	r.mu.Unlock()
	return event, nil
}

func (r *run) snapshotLocked() Invocation {
	response := Invocation{
		InvocationID: r.identity, ActionID: r.action.ID, RuleID: r.action.RuleID,
		Runtime: r.action.Runtime, State: r.state, Execution: r.action.Execution, Error: r.errorText,
	}
	response.Watch = &WatchTarget{
		URL:         "/v1/action-invocations/" + r.identity + "/events?after=" + fmt.Sprint(r.afterCursor),
		ContentType: "application/x-ndjson",
		AfterCursor: r.afterCursor,
	}
	if r.action.Execution.Interruptible && (r.state == StateRunning || r.state == StateCancelling) {
		response.Stop = &StopTarget{Method: "POST", URL: "/v1/action-invocations/" + r.identity + "/stop"}
	}
	return response
}

func newInvocationID(random io.Reader) (string, error) {
	var data [16]byte
	if _, err := io.ReadFull(random, data[:]); err != nil {
		return "", err
	}
	return "act_" + hex.EncodeToString(data[:]), nil
}
