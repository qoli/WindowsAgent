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
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionlaunch"
	"github.com/qoli/WindowsAgent/internal/actionsequence"
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
	ErrRuleSequenceActive = errors.New("an ephemeral Action Sequence already owns this Rule")
	ErrRuleActionActive   = errors.New("the Rule already has an active Action invocation")
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
	ReplayStream(context.Context, uint64, int, string) ([]eventstream.Event, uint64, uint64, error)
	Stream(context.Context, uint64, func(eventstream.Event) error) error
}

type Executor interface {
	RunAction(context.Context, scriptlaunch.Invocation) (actionlaunch.Result, error)
	RunStreaming(context.Context, scriptlaunch.Invocation, streamaction.Reporter) (actionlaunch.Result, error)
	ValidateAction(scriptlaunch.Invocation) (rules.Action, error)
	Contract(string) (actionlaunch.Contract, error)
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
	logger     *slog.Logger

	mu             sync.Mutex
	runs           map[string]*run
	sequenceByRule map[string]string
	activeExternal map[string]uint32
	closed         bool
	wg             sync.WaitGroup
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
	sequence    *actionsequence.Request
}

func NewManager(ruleStore *rules.Store, executor Executor, journal Journal, foregroundSnapshot func() (foreground.Info, error), logger *slog.Logger) (*Manager, error) {
	if ruleStore == nil || executor == nil || journal == nil || foregroundSnapshot == nil || logger == nil {
		return nil, errors.New("Rule store, Action executor, event journal, foreground resolver, and logger are required")
	}
	manager := &Manager{
		rules: ruleStore, executor: executor, journal: journal, foreground: foregroundSnapshot,
		now: time.Now, random: rand.Reader, logger: logger, runs: map[string]*run{},
		sequenceByRule: map[string]string{}, activeExternal: map[string]uint32{},
	}
	recoveryContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.recoverInterrupted(recoveryContext); err != nil {
		return nil, fmt.Errorf("recover interrupted Action invocations: %w", err)
	}
	return manager, nil
}

type interruptedRun struct {
	instance *run
	started  eventstream.Event
	last     eventstream.Event
}

func (m *Manager) recoverInterrupted(ctx context.Context) error {
	pending := map[string]interruptedRun{}
	restartTerminated := map[string]*run{}
	var cursor uint64
	for {
		events, next, last, err := m.journal.ReplayStream(ctx, cursor, eventstream.MaxReplayLimit, StreamName)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Stream != StreamName || event.CorrelationID == "" {
				continue
			}
			switch event.Type {
			case "action.started":
				var payload struct {
					Lifecycle     string `json:"lifecycle"`
					Interruptible bool   `json:"interruptible"`
				}
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					return fmt.Errorf("decode start event %s: %w", event.EventID, err)
				}
				action := rules.Action{
					ID: event.Source.ModuleID, RuleID: event.Foreground.ExecutableName, Runtime: event.Source.Runtime,
					Execution: rules.ActionExecution{Completion: rules.CompletionStream, Lifecycle: payload.Lifecycle, Interruptible: payload.Interruptible},
				}
				instance := &run{
					manager: m, action: action, identity: event.CorrelationID, state: StateFailed,
					ctx: context.Background(), cancel: func() {},
					afterCursor: event.Sequence - 1, lastEventID: event.EventID,
				}
				pending[event.CorrelationID] = interruptedRun{instance: instance, started: event, last: event}
			case "action.completed", "action.failed", "action.cancelled":
				if event.Type == "action.failed" {
					var payload struct {
						Error     string `json:"error"`
						ErrorCode string `json:"errorCode"`
					}
					if err := json.Unmarshal(event.Payload, &payload); err != nil {
						return fmt.Errorf("decode terminal event %s: %w", event.EventID, err)
					}
					if payload.ErrorCode == "ABORTED_BY_AGENT_RESTART" {
						if recovered, ok := pending[event.CorrelationID]; ok {
							recovered.instance.errorText = payload.Error
							recovered.instance.lastEventID = event.EventID
							restartTerminated[event.CorrelationID] = recovered.instance
						}
					}
				}
				delete(pending, event.CorrelationID)
			default:
				if recovered, ok := pending[event.CorrelationID]; ok {
					recovered.last = event
					recovered.instance.lastEventID = event.EventID
					pending[event.CorrelationID] = recovered
				}
			}
		}
		cursor = next
		if cursor >= last {
			break
		}
		if len(events) == 0 {
			return fmt.Errorf("event replay made no progress at cursor %d of %d", cursor, last)
		}
	}
	for identity, instance := range restartTerminated {
		m.runs[identity] = instance
	}
	for identity, recovered := range pending {
		errorText := "streaming Action was interrupted because the Windows Agent process exited"
		recovered.instance.errorText = errorText
		terminal, err := recovered.instance.appendEvent(ctx, "action.failed", map[string]any{
			"state": StateFailed, "error": errorText, "errorCode": "ABORTED_BY_AGENT_RESTART",
			"previousEventId": recovered.last.EventID,
		})
		if err != nil {
			return fmt.Errorf("terminalize interrupted invocation %s: %w", identity, err)
		}
		recovered.instance.lastEventID = terminal.EventID
		m.runs[identity] = recovered.instance
		m.logger.Warn("streaming_action_interrupted_by_restart",
			"invocation_id", identity,
			"action_id", recovered.instance.action.ID,
			"started_at", recovered.started.ObservedAt,
			"last_event_id", recovered.last.EventID,
		)
	}
	return nil
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
	action, err := m.rules.ResolvePublicAction(invocation.Capability)
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve Action %q: %w", invocation.Capability, err)
	}
	if err := m.beginExternal(action.RuleID); err != nil {
		return Invocation{}, err
	}
	defer m.endExternal(action.RuleID)
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

// SequenceToolSchema returns the model-facing strict function schema for one Rule.
func (m *Manager) SequenceToolSchema(ruleID string) (actionsequence.ToolSchema, error) {
	actions, _, err := m.rules.ReadActions(ruleID)
	if err != nil {
		return actionsequence.ToolSchema{}, err
	}
	candidates := make([]actionsequence.Candidate, 0)
	for _, action := range actions {
		if !action.SequenceEligible {
			continue
		}
		contract, err := m.executor.Contract(action.ID)
		if err != nil {
			return actionsequence.ToolSchema{}, err
		}
		candidates = append(candidates, actionsequence.Candidate{
			ID: action.ID, Description: contract.Title, InputSchema: contract.InputSchema,
		})
	}
	return actionsequence.BuildToolSchema(ruleID, candidates)
}

// InvokeSequence validates the complete immutable sequence before starting it.
func (m *Manager) InvokeSequence(ctx context.Context, request actionsequence.Request) (Invocation, error) {
	if m == nil {
		return Invocation{}, errors.New("Action invocation manager is required")
	}
	if ctx == nil {
		return Invocation{}, errors.New("context is required")
	}
	cloned, err := cloneSequenceRequest(request)
	if err != nil {
		return Invocation{}, err
	}
	if err := cloned.Validate(); err != nil {
		return Invocation{}, err
	}
	resolution, err := m.rules.Resolve(cloned.RuleID)
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve Action Sequence Rule %q: %w", cloned.RuleID, err)
	}
	if resolution.Status != rules.StatusMatched {
		return Invocation{}, fmt.Errorf("Action Sequence Rule %q does not exist", cloned.RuleID)
	}
	if resolution.ID != cloned.RuleID {
		return Invocation{}, fmt.Errorf("Action Sequence ruleId must use canonical Rule ID %q", resolution.ID)
	}
	for index := range cloned.Steps {
		step := &cloned.Steps[index]
		contract, err := m.executor.Contract(step.Action)
		if err != nil {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d contract: %w", index+1, err)
		}
		canonicalInputs, err := actionsequence.CanonicalInputs(contract.InputSchema, step.Inputs)
		if err != nil {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d inputs: %w", index+1, err)
		}
		step.Inputs = canonicalInputs
		action, err := m.executor.ValidateAction(scriptlaunch.Invocation{Capability: step.Action, Inputs: step.Inputs})
		if err != nil {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d: %w", index+1, err)
		}
		if action.RuleID != cloned.RuleID {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d: Action %q belongs to Rule %q, expected %q", index+1, action.ID, action.RuleID, cloned.RuleID)
		}
		if !action.SequenceEligible {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d: Action %q is not allowed in an ephemeral Action Sequence", index+1, action.ID)
		}
		if action.Execution.Completion == rules.CompletionStream &&
			(action.Execution.Lifecycle != rules.LifecycleLinear || !action.Execution.Interruptible) {
			return Invocation{}, fmt.Errorf("preflight Action Sequence step %d: streaming Action %q must be linear and interruptible", index+1, action.ID)
		}
	}
	identity, err := newInvocationID(m.random)
	if err != nil {
		return Invocation{}, fmt.Errorf("create Action Sequence invocation ID: %w", err)
	}
	if err := m.reserveSequence(cloned.RuleID, identity); err != nil {
		return Invocation{}, err
	}
	response, err := m.startSequence(cloned, identity)
	if err != nil {
		m.releaseSequence(cloned.RuleID, identity)
		return Invocation{}, err
	}
	return response, nil
}

func cloneSequenceRequest(request actionsequence.Request) (actionsequence.Request, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return actionsequence.Request{}, fmt.Errorf("encode Action Sequence: %w", err)
	}
	var cloned actionsequence.Request
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return actionsequence.Request{}, fmt.Errorf("decode Action Sequence: %w", err)
	}
	return cloned, nil
}

func (m *Manager) reserveSequence(ruleID, identity string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("Action invocation manager is closed")
	}
	if owner := m.sequenceByRule[ruleID]; owner != "" {
		return fmt.Errorf("%w: Rule %s is owned by %s", ErrRuleSequenceActive, ruleID, owner)
	}
	if m.activeExternal[ruleID] != 0 {
		return fmt.Errorf("%w: cannot start Action Sequence for Rule %s while an Action invocation is starting", ErrRuleActionActive, ruleID)
	}
	for _, instance := range m.runs {
		instance.mu.Lock()
		active := strings.EqualFold(instance.action.RuleID, ruleID) &&
			(instance.state == StateRunning || instance.state == StateCancelling)
		instance.mu.Unlock()
		if active {
			return fmt.Errorf("%w: cannot start Action Sequence for Rule %s while invocation %s is active", ErrRuleActionActive, ruleID, instance.identity)
		}
	}
	m.sequenceByRule[ruleID] = identity
	return nil
}

func (m *Manager) releaseSequence(ruleID, identity string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sequenceByRule[ruleID] == identity {
		delete(m.sequenceByRule, ruleID)
	}
}

func (m *Manager) startSequence(request actionsequence.Request, identity string) (Invocation, error) {
	observed, err := m.foreground()
	if err != nil {
		return Invocation{}, fmt.Errorf("resolve foreground before Action Sequence: %w", err)
	}
	if !strings.EqualFold(observed.ExecutableName, request.RuleID) {
		return Invocation{}, fmt.Errorf("foreground executable is %q, expected Action Sequence Rule %q", observed.ExecutableName, request.RuleID)
	}
	action := rules.Action{
		ID: actionsequence.ActionID, RuleID: request.RuleID, Runtime: actionsequence.RuntimeID,
		Execution: rules.ActionExecution{Completion: rules.CompletionStream, Lifecycle: rules.LifecycleLinear, Interruptible: true},
	}
	runContext, cancel := context.WithCancel(context.Background())
	instance := &run{
		manager: m, action: action, invocation: scriptlaunch.Invocation{Capability: action.ID, Inputs: map[string]any{}},
		identity: identity, foreground: observed, ctx: runContext, cancel: cancel, state: StateRunning, sequence: &request,
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
		"interruptible": true,
	})
	if err != nil {
		cancel()
		m.mu.Lock()
		delete(m.runs, identity)
		m.mu.Unlock()
		return Invocation{}, fmt.Errorf("commit Action Sequence start event: %w", err)
	}
	instance.mu.Lock()
	instance.afterCursor = started.Sequence - 1
	instance.mu.Unlock()
	m.wg.Add(1)
	go instance.execute()
	return Invocation{
		InvocationID: identity, ActionID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime,
		State: StateRunning, Execution: action.Execution,
		Watch: &WatchTarget{URL: "/v1/action-invocations/" + identity + "/events?after=" + fmt.Sprint(started.Sequence-1), ContentType: "application/x-ndjson", AfterCursor: started.Sequence - 1},
		Stop:  &StopTarget{Method: "POST", URL: "/v1/action-invocations/" + identity + "/stop"},
	}, nil
}

func (m *Manager) beginExternal(ruleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("Action invocation manager is closed")
	}
	if owner := m.sequenceByRule[ruleID]; owner != "" {
		return fmt.Errorf("%w: Rule %s is owned by %s", ErrRuleSequenceActive, ruleID, owner)
	}
	m.activeExternal[ruleID]++
	return nil
}

func (m *Manager) endExternal(ruleID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeExternal[ruleID] <= 1 {
		delete(m.activeExternal, ruleID)
		return
	}
	m.activeExternal[ruleID]--
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
	defer r.recoverPanic()
	isSequence := r.sequence != nil
	if isSequence {
		defer r.manager.releaseSequence(r.action.RuleID, r.identity)
	}
	var result actionlaunch.Result
	var runErr error
	if isSequence {
		result, runErr = r.executeSequence()
	} else {
		result, runErr = r.manager.executor.RunStreaming(r.ctx, r.invocation, r)
	}
	r.mu.Lock()
	cancelled := r.ctx.Err() != nil
	r.mu.Unlock()
	var state, eventType string
	var payload map[string]any
	switch {
	case cancelled && isSequence && runErr != nil && !errors.Is(runErr, context.Canceled):
		state, eventType = StateFailed, "action.failed"
		payload = map[string]any{"state": state, "error": runErr.Error()}
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
	if isSequence {
		r.sequence = nil
	}
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

func (r *run) executeSequence() (actionlaunch.Result, error) {
	request := r.sequence
	if request == nil {
		return actionlaunch.Result{}, errors.New("Action Sequence request is required")
	}
	if err := r.emitSequenceEvent(r.ctx, actionsequence.EventStarted, actionsequence.StartedEvent{StepCount: len(request.Steps)}); err != nil {
		return actionlaunch.Result{}, err
	}
	for index, step := range request.Steps {
		if err := r.ctx.Err(); err != nil {
			return actionlaunch.Result{}, err
		}
		if err := r.executeSequenceStep(index, step); err != nil {
			return actionlaunch.Result{}, err
		}
	}
	output, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "completedSteps": len(request.Steps), "totalSteps": len(request.Steps),
	})
	if err != nil {
		return actionlaunch.Result{}, fmt.Errorf("encode Action Sequence output: %w", err)
	}
	return actionlaunch.Result{
		ActionID: actionsequence.ActionID, RuleID: request.RuleID, Runtime: actionsequence.RuntimeID, Output: output,
	}, nil
}

func (r *run) executeSequenceStep(index int, step actionsequence.Step) error {
	action, err := r.manager.rules.ResolveAction(step.Action)
	if err != nil {
		return fmt.Errorf("resolve preflighted Action Sequence step %d Action %q: %w", index+1, step.Action, err)
	}
	childExecutionID, err := newInvocationID(r.manager.random)
	if err != nil {
		return fmt.Errorf("create child execution ID for Action Sequence step %d: %w", index+1, err)
	}
	if err := r.emitSequenceEvent(r.ctx, actionsequence.EventStepStarted, actionsequence.StepStartedEvent{
		Step: index + 1, TotalSteps: len(r.sequence.Steps), ActionID: action.ID,
		ChildExecutionID: childExecutionID, Completion: action.Execution.Completion,
	}); err != nil {
		return err
	}
	invocation := scriptlaunch.Invocation{Capability: action.ID, Inputs: step.Inputs}
	if action.Execution.Completion == rules.CompletionReturn {
		result, err := r.manager.executor.RunAction(r.ctx, invocation)
		if err != nil {
			return fmt.Errorf("Action Sequence step %d child Action %s failed: %w", index+1, action.ID, err)
		}
		if len(result.Output) == 0 || !json.Valid(result.Output) {
			return fmt.Errorf("Action Sequence step %d child Action %s returned invalid output", index+1, action.ID)
		}
		if err := r.emitSequenceEvent(r.ctx, actionsequence.EventChildOutput, actionsequence.ChildOutputEvent{
			Step: index + 1, ActionID: action.ID, ChildExecutionID: childExecutionID,
			Output: json.RawMessage(result.Output),
		}); err != nil {
			return err
		}
	} else {
		if err := r.executeStreamingSequenceStep(index, action, invocation, childExecutionID); err != nil {
			return err
		}
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	return r.emitSequenceEvent(r.ctx, actionsequence.EventStepCompleted, actionsequence.StepCompletedEvent{
		Step: index + 1, TotalSteps: len(r.sequence.Steps), ActionID: action.ID, ChildExecutionID: childExecutionID,
	})
}

func (r *run) executeStreamingSequenceStep(index int, action rules.Action, invocation scriptlaunch.Invocation, childExecutionID string) error {
	reporter := sequenceChildReporter{
		parent: r, step: index + 1, actionID: action.ID, childExecutionID: childExecutionID,
	}
	result, err := r.manager.executor.RunStreaming(r.ctx, invocation, reporter)
	if err != nil {
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}
		return fmt.Errorf("Action Sequence step %d child Action %s failed: %w", index+1, action.ID, err)
	}
	if len(result.Output) == 0 || !json.Valid(result.Output) {
		return fmt.Errorf("Action Sequence step %d child Action %s returned invalid output", index+1, action.ID)
	}
	return r.emitSequenceEvent(r.ctx, actionsequence.EventChildOutput, actionsequence.ChildOutputEvent{
		Step: index + 1, ActionID: action.ID, ChildExecutionID: childExecutionID,
		Output: json.RawMessage(result.Output),
	})
}

type sequenceChildReporter struct {
	parent           *run
	step             int
	actionID         string
	childExecutionID string
}

func (r sequenceChildReporter) Emit(ctx context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	if r.parent == nil || r.step <= 0 || r.actionID == "" || r.childExecutionID == "" {
		return eventstream.Event{}, errors.New("Action Sequence child reporter is incomplete")
	}
	if !strings.HasPrefix(eventType, "action.") || eventType == "action.started" || eventType == "action.completed" ||
		eventType == "action.failed" || eventType == "action.cancelled" {
		return eventstream.Event{}, errors.New("Action Sequence child event type must be a non-terminal action.* type")
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return eventstream.Event{}, errors.New("Action Sequence child event payload must be valid JSON")
	}
	wrapped, err := json.Marshal(actionsequence.ChildEvent{
		Step: r.step, ActionID: r.actionID, ChildExecutionID: r.childExecutionID,
		Type: eventType, Payload: json.RawMessage(payload),
	})
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("encode Action Sequence child event: %w", err)
	}
	event, err := r.parent.Emit(ctx, actionsequence.EventChildEvent, wrapped)
	if err != nil {
		return eventstream.Event{}, fmt.Errorf("commit Action Sequence child event: %w", err)
	}
	return event, nil
}

func (r *run) emitSequenceEvent(ctx context.Context, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s: %w", eventType, err)
	}
	if _, err := r.Emit(ctx, eventType, encoded); err != nil {
		return fmt.Errorf("commit %s: %w", eventType, err)
	}
	return nil
}

func (r *run) recoverPanic() {
	recovered := recover()
	if recovered == nil {
		return
	}
	errorText := fmt.Sprintf("streaming Action panicked: %v", recovered)
	r.manager.logger.Error("streaming_action_panicked",
		"invocation_id", r.identity,
		"action_id", r.action.ID,
		"error", errorText,
		"stack", string(debug.Stack()),
	)
	_, appendErr := r.appendEvent(context.Background(), "action.failed", map[string]any{
		"state": StateFailed,
		"error": errorText,
	})
	r.mu.Lock()
	r.sequence = nil
	r.state = StateFailed
	r.errorText = errorText
	if appendErr != nil {
		r.errorText += "; commit terminal Action event: " + appendErr.Error()
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
