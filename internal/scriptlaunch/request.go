// Package scriptlaunch owns the generic request contract accepted by the
// Windows Starlark Script launcher.
package scriptlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const MaxRequestBytes = 256 << 10

type Request struct {
	Inputs map[string]any `json:"inputs"`
}

type Invocation struct {
	Capability string         `json:"capability"`
	Inputs     map[string]any `json:"inputs"`
}

type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

type Executor interface {
	Run(ctx context.Context, invocation Invocation) (json.RawMessage, error)
}

func ReadRequest(name string) (Request, error) {
	if name == "" {
		return Request{}, errors.New("request file is required")
	}
	info, err := os.Stat(name)
	if err != nil {
		return Request{}, fmt.Errorf("stat request file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Request{}, errors.New("request file must be a regular file")
	}
	if info.Size() > MaxRequestBytes {
		return Request{}, fmt.Errorf("request file exceeds %d bytes", MaxRequestBytes)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Request{}, fmt.Errorf("read request file: %w", err)
	}
	var request Request
	if err := decodeStrictJSON(data, &request); err != nil {
		return Request{}, fmt.Errorf("decode request JSON: %w", err)
	}
	if request.Inputs == nil {
		return Request{}, errors.New("request inputs object is required")
	}
	return request, nil
}

func decodeStrictJSON(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func parseLauncherOutput(output []byte, runErr error) (json.RawMessage, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		if runErr != nil {
			return nil, fmt.Errorf("Script launcher exited without JSON output: %w", runErr)
		}
		return nil, errors.New("Script launcher returned empty output")
	}
	if err := strictjson.Validate(output); err != nil {
		return nil, fmt.Errorf("validate Script launcher output: %w", err)
	}
	var envelope struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		ErrorCode  string `json:"errorCode"`
		ErrorStage string `json:"errorStage"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("decode Script launcher output: %w", err)
	}
	if runErr != nil {
		if envelope.Error == "" {
			return nil, fmt.Errorf("Script launcher failed without an error message: %w", runErr)
		}
		cause := fmt.Errorf("Script launcher failed: %s: %w", envelope.Error, runErr)
		if envelope.ErrorCode != "" && envelope.ErrorStage != "" {
			return nil, &Error{Code: envelope.ErrorCode, Stage: envelope.ErrorStage, Cause: cause}
		}
		return nil, cause
	}
	if !envelope.OK {
		return nil, errors.New("Script launcher returned ok=false with exit code zero")
	}
	return append(json.RawMessage(nil), output...), nil
}

type wgcObservationRetryKind string

const (
	wgcObservationRetryObserverEOF    wgcObservationRetryKind = "observer_transport_eof"
	wgcObservationRetryCaptureFailure wgcObservationRetryKind = "screen_capture_failed"
)

func classifyWGCObservationRetry(err error) (wgcObservationRetryKind, bool) {
	var typed *Error
	if !errors.As(err, &typed) {
		return "", false
	}
	if typed.Code == "SCREEN_CAPTURE_FAILED" && typed.Stage == "brokering-observer-call" {
		return wgcObservationRetryCaptureFailure, true
	}
	if typed.Code == "JOB_BROKER_FAILED" && typed.Stage == "brokering-observer-calls" {
		message := typed.Error()
		if strings.Contains(message, "read observer response for screen.readRegion") &&
			strings.Contains(message, "read frame header: EOF") {
			return wgcObservationRetryObserverEOF, true
		}
	}
	return "", false
}

func runWithSilentWGCObservationRetry(
	ctx context.Context,
	capability string,
	maxAttempts int,
	delay time.Duration,
	logger *slog.Logger,
	attempt func() (json.RawMessage, error),
) (json.RawMessage, error) {
	if ctx == nil || capability == "" || maxAttempts <= 0 || delay < 0 || logger == nil || attempt == nil {
		return nil, errors.New("silent WGC observation retry requires context, capability, positive attempts, non-negative delay, logger, and attempt")
	}
	startedAt := time.Now()
	retryCount := 0
	for attemptNumber := 1; attemptNumber <= maxAttempts; attemptNumber++ {
		output, err := attempt()
		if err == nil {
			if retryCount > 0 {
				logger.InfoContext(ctx, "observation_wgc_retry_recovered",
					"capability", capability,
					"attempts", attemptNumber,
					"retry_count", retryCount,
					"elapsed_ms", time.Since(startedAt).Milliseconds(),
				)
			}
			return output, nil
		}
		kind, retryable := classifyWGCObservationRetry(err)
		if !retryable {
			return nil, err
		}
		retryCount++
		if attemptNumber == maxAttempts {
			logger.ErrorContext(ctx, "observation_wgc_retry_exhausted",
				"capability", capability,
				"failure_kind", kind,
				"attempts", attemptNumber,
				"retry_count", retryCount-1,
				"elapsed_ms", time.Since(startedAt).Milliseconds(),
				"error", err,
			)
			return nil, err
		}
		logger.WarnContext(ctx, "observation_wgc_retry_scheduled",
			"capability", capability,
			"failure_kind", kind,
			"failed_attempt", attemptNumber,
			"next_attempt", attemptNumber+1,
			"max_attempts", maxAttempts,
			"elapsed_ms", time.Since(startedAt).Milliseconds(),
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, errors.New("unreachable silent WGC observation retry state")
}
