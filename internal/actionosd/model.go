// Package actionosd reduces explicit streaming Action lifecycle and activity
// events into the small, display-only state consumed by the Windows OSD.
package actionosd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/actionsequence"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/streamaction"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	StatusLive      = "LIVE"
	StatusDone      = "DONE"
	StatusStopped   = "STOPPED"
	StatusFailed    = "FAILED"
	maxActivities   = 3
	maxMessageRunes = 160
)

var (
	doneVisibility   = 3 * time.Second
	failedVisibility = 8 * time.Second
)

type Activity struct {
	ObservedAt time.Time
	Message    string
	Level      string
}

type Snapshot struct {
	Visible      bool
	Status       string
	InvocationID string
	ActionID     string
	StartedAt    time.Time
	TerminalAt   time.Time
	Activities   []Activity
}

type Model struct {
	mu       sync.RWMutex
	current  Snapshot
	sequence *sequenceProjection
}

type sequenceProjection struct {
	totalSteps       int
	step             int
	actionID         string
	childExecutionID string
	active           bool
}

func (m *Model) Apply(event eventstream.Event) error {
	if event.Stream != actionrun.StreamName {
		return nil
	}
	if event.CorrelationID == "" || event.Source.ModuleID == "" {
		return errors.New("Action OSD event requires correlationId and source.moduleId")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch event.Type {
	case "action.started":
		var payload struct {
			State         string `json:"state"`
			ActionID      string `json:"actionId"`
			Lifecycle     string `json:"lifecycle"`
			Interruptible bool   `json:"interruptible"`
		}
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode action.started payload: %w", err)
		}
		if payload.State != actionrun.StateRunning || payload.ActionID != event.Source.ModuleID {
			return errors.New("action.started payload does not match its event source")
		}
		m.current = Snapshot{
			Visible: true, Status: StatusLive, InvocationID: event.CorrelationID,
			ActionID: payload.ActionID, StartedAt: event.ObservedAt,
		}
		m.sequence = nil
	case actionsequence.EventStarted:
		var payload actionsequence.StartedEvent
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Type, err)
		}
		if m.current.InvocationID != event.CorrelationID || m.current.ActionID != actionsequence.ActionID ||
			event.Source.ModuleID != actionsequence.ActionID {
			return errors.New("Action Sequence start does not match the current OSD invocation")
		}
		if payload.StepCount < 1 || payload.StepCount > actionsequence.MaxSteps {
			return fmt.Errorf("Action Sequence stepCount must be from 1 through %d", actionsequence.MaxSteps)
		}
		if m.sequence != nil {
			return errors.New("Action Sequence start is duplicated")
		}
		m.sequence = &sequenceProjection{totalSteps: payload.StepCount}
	case actionsequence.EventStepStarted:
		var payload actionsequence.StepStartedEvent
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Type, err)
		}
		if err := m.validateSequenceStep(event, payload.Step, payload.TotalSteps, payload.ActionID, payload.ChildExecutionID); err != nil {
			return err
		}
		if m.sequence.active || payload.Step != m.sequence.step+1 {
			return fmt.Errorf("Action Sequence step %d is out of order", payload.Step)
		}
		if payload.Completion != rules.CompletionReturn && payload.Completion != rules.CompletionStream {
			return fmt.Errorf("Action Sequence step %d has invalid completion %q", payload.Step, payload.Completion)
		}
		m.sequence.step = payload.Step
		m.sequence.actionID = payload.ActionID
		m.sequence.childExecutionID = payload.ChildExecutionID
		m.sequence.active = true
		m.current.ActionID = payload.ActionID
		m.current.Activities = nil
		m.appendActivityLocked(event.ObservedAt, fmt.Sprintf("Step %d/%d", payload.Step, payload.TotalSteps), "info")
	case actionsequence.EventChildEvent:
		var payload actionsequence.ChildEvent
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Type, err)
		}
		if err := m.validateSequenceChild(event, payload.Step, payload.ActionID, payload.ChildExecutionID); err != nil {
			return err
		}
		if !strings.HasPrefix(payload.Type, "action.") || payload.Type == "action.started" || payload.Type == "action.completed" ||
			payload.Type == "action.failed" || payload.Type == "action.cancelled" {
			return errors.New("Action Sequence child event type must be a non-terminal action.* type")
		}
		if len(payload.Payload) == 0 || !json.Valid(payload.Payload) {
			return errors.New("Action Sequence child event payload must be valid JSON")
		}
		if payload.Type == streamaction.ActivityEventType {
			var activity struct {
				Message string `json:"message"`
				Level   string `json:"level"`
			}
			if err := decodeStrict(payload.Payload, &activity); err != nil {
				return fmt.Errorf("decode wrapped action.activity payload: %w", err)
			}
			if err := validateActivity(activity.Message, activity.Level); err != nil {
				return err
			}
			m.appendActivityLocked(event.ObservedAt, activity.Message, activity.Level)
		}
	case actionsequence.EventChildOutput:
		var payload actionsequence.ChildOutputEvent
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Type, err)
		}
		if err := m.validateSequenceChild(event, payload.Step, payload.ActionID, payload.ChildExecutionID); err != nil {
			return err
		}
		if len(payload.Output) == 0 || !json.Valid(payload.Output) {
			return errors.New("Action Sequence child output must be valid JSON")
		}
	case actionsequence.EventStepCompleted:
		var payload actionsequence.StepCompletedEvent
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode %s payload: %w", event.Type, err)
		}
		if err := m.validateSequenceStep(event, payload.Step, payload.TotalSteps, payload.ActionID, payload.ChildExecutionID); err != nil {
			return err
		}
		if err := m.validateSequenceChild(event, payload.Step, payload.ActionID, payload.ChildExecutionID); err != nil {
			return err
		}
		m.sequence.active = false
	case streamaction.ActivityEventType:
		var payload struct {
			Message string `json:"message"`
			Level   string `json:"level"`
		}
		if err := decodeStrict(event.Payload, &payload); err != nil {
			return fmt.Errorf("decode action.activity payload: %w", err)
		}
		if err := validateActivity(payload.Message, payload.Level); err != nil {
			return err
		}
		if m.current.InvocationID != event.CorrelationID {
			m.current = Snapshot{
				Visible: true, Status: StatusLive, InvocationID: event.CorrelationID,
				ActionID: event.Source.ModuleID, StartedAt: event.ObservedAt,
			}
		}
		m.current.Visible = true
		m.current.Status = StatusLive
		m.current.TerminalAt = time.Time{}
		if count := len(m.current.Activities); count != 0 && m.current.Activities[count-1].Message == payload.Message {
			m.current.Activities[count-1] = Activity{ObservedAt: event.ObservedAt, Message: payload.Message, Level: payload.Level}
			return nil
		}
		m.appendActivityLocked(event.ObservedAt, payload.Message, payload.Level)
	case "action.completed":
		if m.applyTerminal(event, StatusDone) {
			m.sequence = nil
		}
	case "action.cancelled":
		if m.applyTerminal(event, StatusStopped) {
			m.sequence = nil
		}
	case "action.failed":
		if m.applyTerminal(event, StatusFailed) {
			m.sequence = nil
		}
	default:
		if strings.HasPrefix(event.Type, "action.sequence.") {
			return fmt.Errorf("unsupported Action Sequence OSD event type %q", event.Type)
		}
		if strings.HasPrefix(event.Type, "action.") && m.current.InvocationID != event.CorrelationID {
			m.current = Snapshot{
				Visible: true, Status: StatusLive, InvocationID: event.CorrelationID,
				ActionID: event.Source.ModuleID, StartedAt: event.ObservedAt,
			}
		}
	}
	return nil
}

func (m *Model) validateSequenceStep(event eventstream.Event, step, totalSteps int, actionID, childExecutionID string) error {
	if m.sequence == nil || m.current.InvocationID != event.CorrelationID || event.Source.ModuleID != actionsequence.ActionID {
		return errors.New("Action Sequence event does not match the current OSD invocation")
	}
	if totalSteps != m.sequence.totalSteps || step < 1 || step > totalSteps {
		return fmt.Errorf("Action Sequence step %d/%d does not match declared total %d", step, totalSteps, m.sequence.totalSteps)
	}
	if actionID == "" || strings.TrimSpace(actionID) != actionID || childExecutionID == "" || strings.TrimSpace(childExecutionID) != childExecutionID {
		return errors.New("Action Sequence event requires canonical actionId and childExecutionId")
	}
	return nil
}

func (m *Model) validateSequenceChild(event eventstream.Event, step int, actionID, childExecutionID string) error {
	if m.sequence == nil || !m.sequence.active || m.current.InvocationID != event.CorrelationID || event.Source.ModuleID != actionsequence.ActionID {
		return errors.New("Action Sequence child event does not match an active OSD step")
	}
	if step != m.sequence.step || actionID != m.sequence.actionID || childExecutionID != m.sequence.childExecutionID {
		return errors.New("Action Sequence child provenance does not match the active OSD step")
	}
	return nil
}

func (m *Model) appendActivityLocked(observedAt time.Time, message, level string) {
	if count := len(m.current.Activities); count != 0 && m.current.Activities[count-1].Message == message {
		m.current.Activities[count-1] = Activity{ObservedAt: observedAt, Message: message, Level: level}
		return
	}
	m.current.Activities = append(m.current.Activities, Activity{ObservedAt: observedAt, Message: message, Level: level})
	if len(m.current.Activities) > maxActivities {
		m.current.Activities = append([]Activity(nil), m.current.Activities[len(m.current.Activities)-maxActivities:]...)
	}
}

func (m *Model) applyTerminal(event eventstream.Event, status string) bool {
	if m.current.InvocationID != event.CorrelationID {
		return false
	}
	m.current.Status = status
	m.current.Visible = true
	m.current.TerminalAt = event.CommittedAt
	return true
}

func (m *Model) Snapshot(now time.Time) Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := m.current
	result.Activities = append([]Activity(nil), result.Activities...)
	if !result.Visible || result.Status == StatusLive {
		return result
	}
	visibility := doneVisibility
	if result.Status == StatusFailed {
		visibility = failedVisibility
	}
	if result.TerminalAt.IsZero() || !now.Before(result.TerminalAt.Add(visibility)) {
		result.Visible = false
	}
	return result
}

func validateActivity(message, level string) error {
	if message == "" || strings.TrimSpace(message) != message || strings.ContainsAny(message, "\r\n\t") || !utf8.ValidString(message) {
		return errors.New("action.activity message must be one canonical non-empty line")
	}
	if utf8.RuneCountInString(message) > maxMessageRunes {
		return fmt.Errorf("action.activity message exceeds %d characters", maxMessageRunes)
	}
	if level != "info" && level != "warning" && level != "error" {
		return errors.New("action.activity level must equal info, warning, or error")
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are forbidden")
	}
	return nil
}
