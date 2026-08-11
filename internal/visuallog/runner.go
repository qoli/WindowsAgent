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
	Capture     CaptureSource
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
	Capture       CaptureRef    `json:"capture"`
	Model         ModelEvidence `json:"model"`
}

type CaptureRef struct {
	ID string `json:"id"`
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
	if r.Capture == nil || r.Describer == nil || r.Events == nil {
		return errors.New("visual log capture, describer, and event appender are required")
	}
	if r.SessionID == "" || r.InstanceID == "" {
		return errors.New("visual log session and instance identities are required")
	}
	return nil
}

func (r Runner) Warmup(ctx context.Context) error {
	if err := r.Validate(); err != nil {
		return err
	}
	frame, err := r.Capture.Capture(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.drop(DroppedSample{Stage: "warmup_capture", Cause: err})
		return nil
	}
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

func (r Runner) Observe(ctx context.Context) (ObservationResult, error) {
	if err := r.Validate(); err != nil {
		return ObservationResult{}, err
	}
	frame, err := r.Capture.Capture(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ObservationResult{}, ctx.Err()
		}
		dropped := DroppedSample{Stage: "capture", Cause: err}
		r.drop(dropped)
		return ObservationResult{Dropped: &dropped}, nil
	}
	description, err := r.Describer.Describe(ctx, frame)
	if err != nil {
		if ctx.Err() != nil {
			return ObservationResult{}, ctx.Err()
		}
		if appendErr := r.appendFailure(ctx, frame, "model", err); appendErr != nil {
			return ObservationResult{}, fmt.Errorf("describe visual log frame: %v; append failure event: %w", err, appendErr)
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
		Capture:       CaptureRef{ID: frame.CaptureID},
		Model:         ModelEvidence{ID: description.ModelID, LatencyMS: description.Latency.Milliseconds()},
	})
	if err != nil {
		return ObservationResult{}, fmt.Errorf("encode visual log observation: %w", err)
	}
	event, err := r.Events.Append(ctx, r.request(frame, r.Config.Output.ObservationType, payload))
	if err != nil {
		return ObservationResult{}, fmt.Errorf("append visual log observation: %w", err)
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
	if err := r.Warmup(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	if r.OnWarmed != nil {
		r.OnWarmed()
	}
	if _, err := r.Observe(ctx); err != nil {
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
			if _, err := r.Observe(ctx); err != nil {
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
			ExecutableName: frame.Foreground.ExecutableName, Revision: frame.ForegroundRevision,
		},
		Payload:   payload,
		Artifacts: []eventstream.Artifact{{ID: frame.CaptureID, MediaType: frame.ContentType}},
	}
}
