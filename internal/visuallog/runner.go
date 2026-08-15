package visuallog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type EventAppender interface {
	Append(context.Context, eventstream.AppendRequest) (eventstream.Event, error)
}

type Runner struct {
	Config      Config
	Frames      FrameSource
	Describer   Describer
	Events      EventAppender
	SessionID   string
	InstanceID  string
	OnCommitted func(eventstream.Event)
	OnDropped   func(DroppedSample)
	OnWarmed    func()
}

type DroppedSample struct {
	Stage     string
	CaptureID string
	Cause     error
}

type ObservationResult struct {
	Event   *eventstream.Event
	Dropped *DroppedSample
}

type ObservationPayload struct {
	SchemaVersion uint32        `json:"schemaVersion"`
	Timestamp     time.Time     `json:"timestamp"`
	Description   string        `json:"description"`
	Untrusted     bool          `json:"untrusted"`
	Evidence      EvidenceRef   `json:"evidence"`
	Model         ModelEvidence `json:"model"`
}

type EvidenceRef struct {
	CaptureID   string    `json:"captureId"`
	ScheduledAt time.Time `json:"scheduledAt"`
}

type ModelEvidence struct {
	ID        string `json:"id"`
	LatencyMS int64  `json:"latencyMs"`
}

type FailurePayload struct {
	SchemaVersion uint32 `json:"schemaVersion"`
	Stage         string `json:"stage"`
	Error         string `json:"error"`
	CaptureID     string `json:"captureId"`
}

func NewIdentity(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("identity prefix is required")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create %s identity: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(random), nil
}

func (r Runner) Validate() error {
	if err := r.Config.Validate(); err != nil {
		return err
	}
	if r.Frames == nil || r.Describer == nil || r.Events == nil {
		return errors.New("visual log evidence frames, describer, and event appender are required")
	}
	if r.SessionID == "" || r.InstanceID == "" {
		return errors.New("visual log session and instance identities are required")
	}
	return nil
}

func (r Runner) warmup(ctx context.Context, cursor *time.Time) error {
	if err := r.Validate(); err != nil {
		return err
	}
	var frame Frame
	for {
		warmupContext, cancel := context.WithTimeout(ctx, r.Config.WarmupFrameTimeout())
		for {
			var err error
			frame, err = r.Frames.Latest(warmupContext, *cursor)
			if err == nil {
				cancel()
				break
			}
			if ctx.Err() != nil {
				cancel()
				return ctx.Err()
			}
			delay := r.Config.Interval()
			if !errors.Is(err, ErrNoNewEvidenceFrame) {
				r.drop(DroppedSample{Stage: "warmup_evidence", Cause: err})
			}
			timer := time.NewTimer(delay)
			select {
			case <-warmupContext.Done():
				timer.Stop()
				cancel()
			case <-timer.C:
				continue
			}
			break
		}
		if !frame.ScheduledAt.IsZero() {
			break
		}
	}
	*cursor = frame.ScheduledAt
	for call := uint32(1); call <= r.Config.WarmupCalls; call++ {
		if _, err := r.Describer.Describe(ctx, frame); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.drop(DroppedSample{Stage: "warmup_model", CaptureID: frame.CaptureID, Cause: fmt.Errorf("call %d: %w", call, err)})
		}
	}
	return nil
}

func (r Runner) observe(ctx context.Context, cursor *time.Time) (ObservationResult, error) {
	if err := r.Validate(); err != nil {
		return ObservationResult{}, err
	}
	frame, err := r.Frames.Latest(ctx, *cursor)
	if err != nil {
		if ctx.Err() != nil {
			return ObservationResult{}, ctx.Err()
		}
		if errors.Is(err, ErrNoNewEvidenceFrame) {
			return ObservationResult{}, nil
		}
		dropped := DroppedSample{Stage: "evidence", Cause: err}
		r.drop(dropped)
		return ObservationResult{Dropped: &dropped}, nil
	}
	*cursor = frame.ScheduledAt
	description, err := r.Describer.Describe(ctx, frame)
	if err != nil {
		if ctx.Err() != nil {
			return ObservationResult{}, ctx.Err()
		}
		if appendErr := r.appendFailure(ctx, frame, "model", err); appendErr != nil {
			dropped := DroppedSample{Stage: "journal", CaptureID: frame.CaptureID, Cause: fmt.Errorf("record model failure after %v: %w", err, appendErr)}
			r.drop(dropped)
			return ObservationResult{Dropped: &dropped}, nil
		}
		dropped := DroppedSample{Stage: "model", CaptureID: frame.CaptureID, Cause: err}
		r.drop(dropped)
		return ObservationResult{Dropped: &dropped}, nil
	}
	payload, err := json.Marshal(ObservationPayload{
		SchemaVersion: SchemaVersion,
		Timestamp:     frame.ObservedAt,
		Description:   description.Text,
		Untrusted:     true,
		Evidence:      EvidenceRef{CaptureID: frame.CaptureID, ScheduledAt: frame.ScheduledAt},
		Model:         ModelEvidence{ID: description.ModelID, LatencyMS: description.Latency.Milliseconds()},
	})
	if err != nil {
		return ObservationResult{}, fmt.Errorf("encode visual log observation: %w", err)
	}
	event, err := r.Events.Append(ctx, r.request(frame, r.Config.Output.ObservationType, payload))
	if err != nil {
		if ctx.Err() != nil {
			return ObservationResult{}, ctx.Err()
		}
		dropped := DroppedSample{Stage: "journal", CaptureID: frame.CaptureID, Cause: fmt.Errorf("append visual log observation: %w", err)}
		r.drop(dropped)
		return ObservationResult{Dropped: &dropped}, nil
	}
	if r.OnCommitted != nil {
		r.OnCommitted(event)
	}
	return ObservationResult{Event: &event}, nil
}

func (r Runner) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("visual log context is required")
	}
	var cursor time.Time
	if err := r.warmup(ctx, &cursor); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if r.OnWarmed != nil {
		r.OnWarmed()
	}
	if _, err := r.observe(ctx, &cursor); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	ticker := time.NewTicker(r.Config.Interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := r.observe(ctx, &cursor); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (r Runner) drop(sample DroppedSample) {
	if r.OnDropped != nil {
		r.OnDropped(sample)
	}
}

func (r Runner) appendFailure(ctx context.Context, frame Frame, stage string, cause error) error {
	payload, err := json.Marshal(FailurePayload{
		SchemaVersion: SchemaVersion, Stage: stage, Error: boundedError(cause), CaptureID: frame.CaptureID,
	})
	if err != nil {
		return err
	}
	_, err = r.Events.Append(ctx, r.request(frame, r.Config.Output.FailureType, payload))
	return err
}

func boundedError(cause error) string {
	const maximum = 2048
	message := cause.Error()
	if len(message) <= maximum {
		return message
	}
	return message[:maximum]
}

func (r Runner) request(frame Frame, eventType string, payload json.RawMessage) eventstream.AppendRequest {
	return eventstream.AppendRequest{
		SessionID:  r.SessionID,
		Stream:     r.Config.Output.Stream,
		Type:       eventType,
		ObservedAt: frame.ObservedAt,
		Source: eventstream.Source{
			ModuleID: r.Config.ModuleID, InstanceID: r.InstanceID, Runtime: r.Config.Runtime,
		},
		Foreground: eventstream.Foreground{
			ExecutableName: frame.ForegroundExecutable, Revision: frame.ForegroundRevision,
		},
		Payload:   payload,
		Artifacts: []eventstream.Artifact{{ID: frame.CaptureID, MediaType: frame.ContentType}},
	}
}
