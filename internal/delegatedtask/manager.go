// Package delegatedtask owns the durable lifecycle of work delegated to PI WEB.
package delegatedtask

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

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/piweb"
)

const StreamName = "delegated.tasks"

const (
	StateStarting     = "STARTING"
	StateRunning      = "RUNNING"
	StateWaitingInput = "WAITING_INPUT"
	StateCancelling   = "CANCELLING"
	StateCompleted    = "COMPLETED"
	StateFailed       = "FAILED"
	StateCancelled    = "CANCELLED"
)

var ErrTaskNotFound = errors.New("delegated task not found")

type eventHandlingError struct {
	code string
	err  error
}

func (failure *eventHandlingError) Error() string { return failure.err.Error() }
func (failure *eventHandlingError) Unwrap() error { return failure.err }

type Journal interface {
	Append(context.Context, eventstream.AppendRequest) (eventstream.Event, error)
	ReadStreamAfter(context.Context, uint64, string, int) ([]eventstream.Event, uint64, error)
	WaitAfter(context.Context, uint64, int) ([]eventstream.Event, error)
	LastSequence() (uint64, error)
}

type Runtime interface {
	StartSession(context.Context, string) (piweb.Session, error)
	Subscribe(context.Context, piweb.Session) (piweb.EventStream, error)
	Prompt(context.Context, piweb.Session, string, string) error
	Abort(context.Context, piweb.Session) error
	WebURL(piweb.Session) string
}

type SubmitRequest struct {
	Prompt string `json:"prompt"`
	CWD    string `json:"cwd"`
}

type MessageRequest struct {
	Text string `json:"text"`
	Mode string `json:"mode"`
}

type Task struct {
	TaskID       string `json:"taskId"`
	State        string `json:"state"`
	CWD          string `json:"cwd"`
	SessionID    string `json:"sessionId,omitempty"`
	PiWebURL     string `json:"piWebUrl,omitempty"`
	LastSequence uint64 `json:"lastSequence"`
	ErrorCode    string `json:"errorCode,omitempty"`
	Error        string `json:"error,omitempty"`
}

type Manager struct {
	journal Journal
	runtime Runtime
	now     func() time.Time
	random  io.Reader

	mu     sync.Mutex
	tasks  map[string]*taskRun
	closed bool
}

type taskRun struct {
	manager *Manager
	ctx     context.Context
	cancel  context.CancelFunc

	eventMu            sync.Mutex
	awaitingIdleStatus bool
	attention          map[string]string
	mu                 sync.Mutex
	task               Task
}

func NewManager(journal Journal, runtime Runtime) (*Manager, error) {
	if journal == nil || runtime == nil {
		return nil, errors.New("delegated task journal and PI WEB runtime are required")
	}
	manager := &Manager{journal: journal, runtime: runtime, now: time.Now, random: rand.Reader, tasks: map[string]*taskRun{}}
	if err := manager.recover(context.Background()); err != nil {
		return nil, fmt.Errorf("recover delegated tasks: %w", err)
	}
	return manager, nil
}

func (m *Manager) Submit(ctx context.Context, request SubmitRequest) (Task, error) {
	if ctx == nil {
		return Task{}, errors.New("context is required")
	}
	if strings.TrimSpace(request.Prompt) == "" || !isAbsoluteWindowsPath(request.CWD) {
		return Task{}, errors.New("prompt must not be blank and cwd must be an absolute Windows path")
	}
	id, err := newTaskID(m.random)
	if err != nil {
		return Task{}, fmt.Errorf("create delegated task ID: %w", err)
	}
	runContext, cancel := context.WithCancel(context.Background())
	run := &taskRun{manager: m, ctx: runContext, cancel: cancel, attention: map[string]string{}, task: Task{TaskID: id, State: StateStarting, CWD: request.CWD}}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return Task{}, errors.New("delegated task manager is closed")
	}
	m.tasks[id] = run
	m.mu.Unlock()
	if err := run.append(ctx, "task.created", map[string]any{"state": StateStarting, "cwd": request.CWD}); err != nil {
		m.remove(id)
		cancel()
		return Task{}, err
	}

	session, err := m.runtime.StartSession(ctx, request.CWD)
	if err != nil {
		run.fail("PI_WEB_SESSION_START_FAILED", err)
		return run.snapshot(), nil
	}
	run.mu.Lock()
	run.task.CWD = session.CWD
	run.task.SessionID = session.ID
	run.task.PiWebURL = m.runtime.WebURL(session)
	run.mu.Unlock()
	stream, err := m.runtime.Subscribe(runContext, session)
	if err != nil {
		run.fail("PI_WEB_EVENT_SUBSCRIBE_FAILED", err)
		return run.snapshot(), nil
	}
	if err := m.runtime.Prompt(ctx, session, request.Prompt, ""); err != nil {
		run.fail("PI_WEB_PROMPT_FAILED", err)
		return run.snapshot(), nil
	}
	run.setState(StateRunning)
	if err := run.append(ctx, "task.started", map[string]any{
		"state": StateRunning, "cwd": session.CWD, "sessionId": session.ID, "piWebUrl": m.runtime.WebURL(session),
	}); err != nil {
		cancel()
		abortErr := m.runtime.Abort(context.Background(), session)
		if abortErr != nil {
			return Task{}, fmt.Errorf("commit delegated task start: %v; abort untracked PI WEB session: %w", err, abortErr)
		}
		return Task{}, fmt.Errorf("commit delegated task start: %w", err)
	}
	go run.consume(session, stream)
	return run.snapshot(), nil
}

func (m *Manager) Get(taskID string) (Task, error) {
	run, err := m.lookup(taskID)
	if err != nil {
		return Task{}, err
	}
	return run.snapshot(), nil
}

func (m *Manager) Send(ctx context.Context, taskID string, request MessageRequest) (Task, error) {
	if strings.TrimSpace(request.Text) == "" || (request.Mode != "steer" && request.Mode != "followUp") {
		return Task{}, errors.New("text is required and mode must be steer or followUp")
	}
	run, err := m.lookup(taskID)
	if err != nil {
		return Task{}, err
	}
	run.eventMu.Lock()
	defer run.eventMu.Unlock()
	task := run.snapshot()
	if task.State != StateRunning {
		return Task{}, fmt.Errorf("delegated task is not accepting messages in state %s", task.State)
	}
	session := piweb.Session{ID: task.SessionID, CWD: task.CWD}
	if err := m.runtime.Prompt(ctx, session, request.Text, request.Mode); err != nil {
		return Task{}, fmt.Errorf("send PI WEB message: %w", err)
	}
	run.awaitingIdleStatus = false
	if err := run.append(ctx, "task.message.accepted", map[string]any{"state": StateRunning, "mode": request.Mode}); err != nil {
		return Task{}, err
	}
	run.setState(StateRunning)
	return run.snapshot(), nil
}

func (m *Manager) Cancel(ctx context.Context, taskID string) (Task, error) {
	run, err := m.lookup(taskID)
	if err != nil {
		return Task{}, err
	}
	run.eventMu.Lock()
	defer run.eventMu.Unlock()
	task := run.snapshot()
	if terminal(task.State) {
		return Task{}, fmt.Errorf("delegated task is already terminal in state %s", task.State)
	}
	hadEnded := run.awaitingIdleStatus
	if err := run.append(ctx, "task.cancellation.requested", map[string]any{"state": StateCancelling}); err != nil {
		return Task{}, err
	}
	run.setState(StateCancelling)
	if err := m.runtime.Abort(ctx, piweb.Session{ID: task.SessionID, CWD: task.CWD}); err != nil {
		run.failLocked("PI_WEB_ABORT_FAILED", err)
		return run.snapshot(), nil
	}
	if hadEnded {
		if err := run.append(ctx, "task.cancelled", map[string]any{"state": StateCancelled, "reason": "abort_confirmed_after_agent_end"}); err != nil {
			return Task{}, err
		}
		run.setState(StateCancelled)
		run.cancel()
	}
	return run.snapshot(), nil
}

func (m *Manager) Events(ctx context.Context, taskID string, after uint64, limit int) ([]eventstream.Event, uint64, uint64, error) {
	if limit < 1 || limit > eventstream.MaxReplayLimit {
		return nil, 0, 0, fmt.Errorf("event replay limit must be between 1 and %d", eventstream.MaxReplayLimit)
	}
	if _, err := m.lookup(taskID); err != nil {
		return nil, 0, 0, err
	}
	last, err := m.journal.LastSequence()
	if err != nil {
		return nil, 0, 0, err
	}
	if after > last {
		return nil, 0, last, fmt.Errorf("%w: cursor=%d lastSequence=%d", eventstream.ErrCursorAhead, after, last)
	}
	events := make([]eventstream.Event, 0, limit)
	cursor := after
	for cursor < last && len(events) < limit {
		batch, next, err := m.journal.ReadStreamAfter(ctx, cursor, StreamName, limit-len(events))
		if err != nil {
			return nil, 0, 0, err
		}
		for _, event := range batch {
			if event.CorrelationID == taskID {
				events = append(events, event)
			}
		}
		if next <= cursor {
			return nil, 0, 0, errors.New("delegated task event replay did not advance")
		}
		cursor = next
	}
	return events, cursor, last, nil
}

func (m *Manager) WaitEvents(ctx context.Context, taskID string, after uint64) ([]eventstream.Event, error) {
	if _, err := m.lookup(taskID); err != nil {
		return nil, err
	}
	for {
		events, err := m.journal.WaitAfter(ctx, after, eventstream.DefaultReplayLimit)
		if err != nil {
			return nil, err
		}
		matched := make([]eventstream.Event, 0, len(events))
		for _, event := range events {
			after = event.Sequence
			if event.Stream == StreamName && event.CorrelationID == taskID {
				matched = append(matched, event)
			}
		}
		if len(matched) > 0 {
			return matched, nil
		}
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, run := range m.tasks {
		run.cancel()
	}
	m.mu.Unlock()
}

func (m *Manager) recover(ctx context.Context) error {
	last, err := m.journal.LastSequence()
	if err != nil {
		return err
	}
	var cursor uint64
	for cursor < last {
		events, next, err := m.journal.ReadStreamAfter(ctx, cursor, StreamName, eventstream.MaxReplayLimit)
		if err != nil {
			return err
		}
		for _, event := range events {
			var payload struct {
				State     string `json:"state"`
				CWD       string `json:"cwd"`
				SessionID string `json:"sessionId"`
				PiWebURL  string `json:"piWebUrl"`
				ErrorCode string `json:"errorCode"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode delegated task event %s: %w", event.EventID, err)
			}
			run := m.tasks[event.CorrelationID]
			if run == nil {
				runContext, cancel := context.WithCancel(context.Background())
				run = &taskRun{manager: m, ctx: runContext, cancel: cancel, attention: map[string]string{}, task: Task{TaskID: event.CorrelationID}}
				m.tasks[event.CorrelationID] = run
			}
			if payload.CWD != "" {
				run.task.CWD = payload.CWD
			}
			if payload.State != "" {
				run.task.State = payload.State
			}
			if payload.SessionID != "" {
				run.task.SessionID = payload.SessionID
			}
			if payload.PiWebURL != "" {
				run.task.PiWebURL = payload.PiWebURL
			}
			run.task.ErrorCode, run.task.Error, run.task.LastSequence = payload.ErrorCode, payload.Error, event.Sequence
		}
		if next <= cursor {
			return errors.New("delegated task recovery did not advance")
		}
		cursor = next
	}
	for _, run := range m.tasks {
		if !terminal(run.task.State) {
			run.fail("ABORTED_BY_RUNTIME_RESTART", errors.New("delegated task was interrupted because windows-agent-pi exited"))
		}
	}
	return nil
}

func (run *taskRun) consume(session piweb.Session, stream piweb.EventStream) {
	for stream.Events != nil || stream.Errors != nil {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				stream.Events = nil
				continue
			}
			if err := run.handleEvent(event); err != nil {
				code := "PI_WEB_EVENT_INVALID"
				var handling *eventHandlingError
				if errors.As(err, &handling) {
					code = handling.code
				}
				run.fail(code, err)
				return
			}
		case err, ok := <-stream.Errors:
			if !ok {
				stream.Errors = nil
				continue
			}
			if err != nil && !run.manager.isClosed() && !terminal(run.snapshot().State) {
				run.fail("PI_WEB_EVENT_STREAM_FAILED", err)
			}
			return
		case <-run.ctx.Done():
			return
		}
	}
	if !run.manager.isClosed() && !terminal(run.snapshot().State) {
		run.fail("PI_WEB_EVENT_STREAM_ENDED", errors.New("PI WEB event stream ended before a terminal agent event"))
	}
}

func (run *taskRun) handleEvent(event piweb.Event) error {
	run.eventMu.Lock()
	defer run.eventMu.Unlock()
	state := run.snapshot().State
	switch event.Type {
	case "ask.opened", "dialog.opened":
		key, reason, err := attentionIdentity(event)
		if err != nil {
			return invalidPiEvent(err)
		}
		run.attention[key] = reason
		state = StateWaitingInput
		if err := run.appendPiEvent("task.attention.required", map[string]any{
			"state": state, "reason": reason, "piWebUrl": run.snapshot().PiWebURL,
		}); err != nil {
			return err
		}
		run.setState(state)
		return nil
	case "ask.closed", "dialog.closed":
		key, reason, err := attentionIdentity(event)
		if err != nil {
			return invalidPiEvent(err)
		}
		delete(run.attention, key)
		state = StateRunning
		if len(run.attention) > 0 {
			state = StateWaitingInput
		}
		if err := run.appendPiEvent("task.attention.resolved", map[string]any{
			"state": state, "reason": reason, "piWebUrl": run.snapshot().PiWebURL,
		}); err != nil {
			return err
		}
		run.setState(state)
		return nil
	case "agent.start":
		state = StateRunning
		if len(run.attention) > 0 {
			state = StateWaitingInput
		}
		run.awaitingIdleStatus = false
	case "status.update":
		if run.awaitingIdleStatus && state != StateCancelling && len(run.attention) == 0 {
			idle, err := idleStatus(event.Raw)
			if err != nil {
				return invalidPiEvent(err)
			}
			if idle {
				if err := run.appendPiEvent("task.completed", map[string]any{"state": StateCompleted, "piEvent": event.Raw}); err != nil {
					return err
				}
				run.setState(StateCompleted)
				run.cancel()
				return nil
			}
		}
	case "session.error":
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(event.Raw, &payload); err != nil {
			return invalidPiEvent(err)
		}
		run.failLocked("PI_WEB_SESSION_ERROR", errors.New(payload.Message))
		return nil
	case "agent.end":
		if state == StateCancelling {
			if err := run.appendPiEvent("task.cancelled", map[string]any{"state": StateCancelled, "piEvent": event.Raw}); err != nil {
				return err
			}
			run.setState(StateCancelled)
		} else {
			run.awaitingIdleStatus = true
			if err := run.appendPiEvent("task.progress", map[string]any{"state": StateRunning, "piEventType": event.Type, "piSequence": event.Seq, "piEvent": event.Raw}); err != nil {
				return err
			}
			return nil
		}
		run.cancel()
		return nil
	}
	if err := run.appendPiEvent("task.progress", map[string]any{"state": state, "piEventType": event.Type, "piSequence": event.Seq, "piEvent": event.Raw}); err != nil {
		return err
	}
	run.setState(state)
	return nil
}

func (run *taskRun) appendPiEvent(eventType string, payload any) error {
	if err := run.append(context.Background(), eventType, payload); err != nil {
		return &eventHandlingError{code: "TASK_EVENT_JOURNAL_FAILED", err: err}
	}
	return nil
}

func invalidPiEvent(err error) error {
	return &eventHandlingError{code: "PI_WEB_EVENT_INVALID", err: err}
}

func attentionIdentity(event piweb.Event) (string, string, error) {
	var envelope struct {
		Ask struct {
			ID string `json:"askId"`
		} `json:"ask"`
		AskID  string `json:"askId"`
		Dialog struct {
			ID string `json:"dialogId"`
		} `json:"dialog"`
		DialogID string `json:"dialogId"`
	}
	if err := json.Unmarshal(event.Raw, &envelope); err != nil {
		return "", "", fmt.Errorf("decode PI WEB attention event: %w", err)
	}
	switch event.Type {
	case "ask.opened":
		if envelope.Ask.ID == "" {
			return "", "", errors.New("PI WEB ask.opened event is missing askId")
		}
		return "ask:" + envelope.Ask.ID, "pi.ask_user", nil
	case "ask.closed":
		if envelope.AskID == "" {
			return "", "", errors.New("PI WEB ask.closed event is missing askId")
		}
		return "ask:" + envelope.AskID, "pi.ask_user", nil
	case "dialog.opened":
		if envelope.Dialog.ID == "" {
			return "", "", errors.New("PI WEB dialog.opened event is missing dialogId")
		}
		return "dialog:" + envelope.Dialog.ID, "pi.extension_dialog", nil
	case "dialog.closed":
		if envelope.DialogID == "" {
			return "", "", errors.New("PI WEB dialog.closed event is missing dialogId")
		}
		return "dialog:" + envelope.DialogID, "pi.extension_dialog", nil
	default:
		return "", "", fmt.Errorf("PI WEB event type %s is not an attention event", event.Type)
	}
}

func idleStatus(raw json.RawMessage) (bool, error) {
	var event struct {
		Status struct {
			IsStreaming         *bool `json:"isStreaming"`
			IsCompacting        *bool `json:"isCompacting"`
			IsBashRunning       *bool `json:"isBashRunning"`
			PendingMessageCount *int  `json:"pendingMessageCount"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return false, fmt.Errorf("decode PI WEB status event: %w", err)
	}
	status := event.Status
	if status.IsStreaming == nil || status.IsCompacting == nil || status.IsBashRunning == nil || status.PendingMessageCount == nil || *status.PendingMessageCount < 0 {
		return false, errors.New("PI WEB status event is missing required lifecycle fields")
	}
	return !*status.IsStreaming && !*status.IsCompacting && !*status.IsBashRunning && *status.PendingMessageCount == 0, nil
}

func (run *taskRun) fail(code string, cause error) {
	run.eventMu.Lock()
	defer run.eventMu.Unlock()
	run.failLocked(code, cause)
}

func (run *taskRun) failLocked(code string, cause error) {
	run.mu.Lock()
	if terminal(run.task.State) {
		run.mu.Unlock()
		return
	}
	run.mu.Unlock()
	appendErr := run.append(context.Background(), "task.failed", map[string]any{"state": StateFailed, "errorCode": code, "error": cause.Error()})
	run.mu.Lock()
	run.task.State, run.task.ErrorCode, run.task.Error = StateFailed, code, cause.Error()
	if appendErr != nil {
		run.task.Error = cause.Error() + "; commit terminal event: " + appendErr.Error()
	}
	run.mu.Unlock()
	run.cancel()
}

func (run *taskRun) append(ctx context.Context, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	unavailable := false
	event, err := run.manager.journal.Append(ctx, eventstream.AppendRequest{
		SessionID: run.task.TaskID, Stream: StreamName, Type: eventType, ObservedAt: run.manager.now().UTC(),
		Source:     eventstream.Source{ModuleID: "windows-agent-pi", InstanceID: "delegated-task-manager", Runtime: "pi-web"},
		Foreground: eventstream.Foreground{Available: &unavailable}, CorrelationID: run.task.TaskID, Payload: encoded,
	})
	if err != nil {
		return err
	}
	run.mu.Lock()
	run.task.LastSequence = event.Sequence
	run.mu.Unlock()
	return nil
}

func (run *taskRun) setState(state string) {
	run.mu.Lock()
	run.task.State = state
	run.mu.Unlock()
}

func (run *taskRun) snapshot() Task {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.task
}

func (m *Manager) lookup(taskID string) (*taskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.tasks[taskID]
	if run == nil {
		return nil, ErrTaskNotFound
	}
	return run, nil
}

func (m *Manager) remove(taskID string) {
	m.mu.Lock()
	delete(m.tasks, taskID)
	m.mu.Unlock()
}

func (m *Manager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func terminal(state string) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled
}

func isAbsoluteWindowsPath(path string) bool {
	if path == "" || strings.TrimSpace(path) != path {
		return false
	}
	if len(path) >= 2 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' {
		return len(path) >= 3 && (path[2] == '\\' || path[2] == '/')
	}
	if !strings.HasPrefix(path, `\\`) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, `\\`), `\`)
	return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
}

func newTaskID(reader io.Reader) (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return "task_" + hex.EncodeToString(data), nil
}
