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
	"github.com/qoli/WindowsAgent/internal/eventstream"
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
	mu      sync.RWMutex
	current Snapshot
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
		m.current.Activities = append(m.current.Activities, Activity{ObservedAt: event.ObservedAt, Message: payload.Message, Level: payload.Level})
		if len(m.current.Activities) > maxActivities {
			m.current.Activities = append([]Activity(nil), m.current.Activities[len(m.current.Activities)-maxActivities:]...)
		}
	case "action.completed":
		m.applyTerminal(event, StatusDone)
	case "action.cancelled":
		m.applyTerminal(event, StatusStopped)
	case "action.failed":
		m.applyTerminal(event, StatusFailed)
	default:
		if strings.HasPrefix(event.Type, "action.") && m.current.InvocationID != event.CorrelationID {
			m.current = Snapshot{
				Visible: true, Status: StatusLive, InvocationID: event.CorrelationID,
				ActionID: event.Source.ModuleID, StartedAt: event.ObservedAt,
			}
		}
	}
	return nil
}

func (m *Model) applyTerminal(event eventstream.Event, status string) {
	if m.current.InvocationID != event.CorrelationID {
		return
	}
	m.current.Status = status
	m.current.Visible = true
	m.current.TerminalAt = event.CommittedAt
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
