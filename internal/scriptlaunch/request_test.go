package scriptlaunch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadRequest(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(name, []byte(`{
	  "inputs": {"value": 7}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := ReadRequest(name)
	if err != nil {
		t.Fatal(err)
	}
	if request.Inputs["value"] != float64(7) {
		t.Fatalf("request = %#v", request)
	}
}

func TestReadRequestAcceptsBoundedTextRegionPayload(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	payload := `{"inputs":{"pixels":"` + strings.Repeat("a", 128<<10) + `"}}`
	if err := os.WriteFile(name, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(name); err != nil {
		t.Fatalf("bounded text-region payload was rejected: %v", err)
	}
}

func TestReadRequestRejectsPayloadOverBound(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	payload := `{"inputs":{"pixels":"` + strings.Repeat("a", MaxRequestBytes) + `"}}`
	if err := os.WriteFile(name, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(name); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized request error = %v", err)
	}
}

func TestReadRequestRejectsInvalidContract(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing inputs", body: `{}`, want: "inputs"},
		{name: "removed roots", body: `{"inputs":{},"fileRoots":{}}`, want: "unknown"},
		{name: "unknown field", body: `{"inputs":{},"extra":true}`, want: "unknown"},
		{name: "duplicate key", body: `{"inputs":{},"inputs":{}}`, want: "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(name, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRequest(name); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadRequest() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseLauncherOutputPreservesSuccessfulEnvelope(t *testing.T) {
	input := []byte(`{
	  "ok": true,
	  "jobId": "job",
	  "capability": "game/status",
	  "ruleId": "game.exe",
	  "output": {"status": "ok"},
	  "provenance": []
	}`)
	output, err := parseLauncherOutput(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["capability"] != "game/status" {
		t.Fatalf("output = %#v", decoded)
	}
}

func TestParseLauncherOutputRequiresErrorOnFailedExit(t *testing.T) {
	if _, err := parseLauncherOutput(
		[]byte(`{"ok":false,"error":"owning Rule mismatch"}`),
		errors.New("exit status 1"),
	); err == nil || !strings.Contains(err.Error(), "owning Rule mismatch") {
		t.Fatalf("parseLauncherOutput() error = %v", err)
	}
}

func TestParseLauncherOutputPreservesStructuredFailure(t *testing.T) {
	_, err := parseLauncherOutput(
		[]byte(`{"ok":false,"error":"deadline","errorCode":"JOB_DEADLINE_EXCEEDED","errorStage":"executing-script"}`),
		errors.New("exit status 1"),
	)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "JOB_DEADLINE_EXCEEDED" || typed.Stage != "executing-script" {
		t.Fatalf("error = %#v", err)
	}
}

func TestRetryableWGCObservationFailureIsNarrow(t *testing.T) {
	retryable := &Error{
		Code:  "JOB_BROKER_FAILED",
		Stage: "brokering-observer-calls",
		Cause: errors.New("read observer response for screen.readRegion: read frame header: EOF"),
	}
	if kind, ok := classifyWGCObservationRetry(retryable); !ok || kind != wgcObservationRetryObserverEOF {
		t.Fatal("exact screen.readRegion observer EOF must be retryable")
	}
	captureFailure := &Error{
		Code:  "SCREEN_CAPTURE_FAILED",
		Stage: "brokering-observer-call",
		Cause: errors.New("screen.readRegion returned SCREEN_CAPTURE_FAILED"),
	}
	if kind, ok := classifyWGCObservationRetry(captureFailure); !ok || kind != wgcObservationRetryCaptureFailure {
		t.Fatalf("screen capture failure classification = %q, %v", kind, ok)
	}
	for _, err := range []error{
		&Error{Code: "JOB_BROKER_FAILED", Stage: "brokering-observer-calls", Cause: errors.New("read observer response for file.read: read frame header: EOF")},
		&Error{Code: "SCREEN_CAPTURE_FAILED", Stage: "executing-script", Cause: errors.New("screen.readRegion returned SCREEN_CAPTURE_FAILED")},
		&Error{Code: "JOB_DEADLINE_EXCEEDED", Stage: "executing-script", Cause: errors.New("read observer response for screen.readRegion: read frame header: EOF")},
		errors.New("read observer response for screen.readRegion: read frame header: EOF"),
	} {
		if _, retryable := classifyWGCObservationRetry(err); retryable {
			t.Fatalf("unrelated failure became retryable: %v", err)
		}
	}
}

func TestLauncherScreenCaptureEnvelopeIsRetryable(t *testing.T) {
	_, err := parseLauncherOutput(
		[]byte(`{"ok":false,"error":"capture failed","errorCode":"SCREEN_CAPTURE_FAILED","errorStage":"brokering-observer-call"}`),
		errors.New("exit status 1"),
	)
	if kind, ok := classifyWGCObservationRetry(err); !ok || kind != wgcObservationRetryCaptureFailure {
		t.Fatalf("launcher envelope classification = %q, %v; error=%v", kind, ok, err)
	}
}

func TestSilentWGCObservationRetryReplaysWholeReadOnlyCapability(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	attempts := 0
	output, err := runWithSilentWGCObservationRetry(
		context.Background(),
		"game/read-screen",
		5,
		0,
		logger,
		func() (json.RawMessage, error) {
			attempts++
			if attempts < 3 {
				return nil, &Error{Code: "SCREEN_CAPTURE_FAILED", Stage: "brokering-observer-call", Cause: errors.New("WGC unavailable")}
			}
			return json.RawMessage(`{"ok":true}`), nil
		},
	)
	if err != nil || string(output) != `{"ok":true}` || attempts != 3 {
		t.Fatalf("output=%s attempts=%d error=%v", output, attempts, err)
	}
	for _, marker := range []string{"observation_wgc_retry_scheduled", "observation_wgc_retry_recovered", "capability=game/read-screen"} {
		if !strings.Contains(logs.String(), marker) {
			t.Fatalf("missing log marker %q in %s", marker, logs.String())
		}
	}
}

func TestSilentWGCObservationRetryDoesNotReplayNonWGCFailure(t *testing.T) {
	attempts := 0
	want := &Error{Code: "FOREGROUND_CHANGED", Stage: "brokering-observer-call", Cause: errors.New("foreground changed")}
	_, err := runWithSilentWGCObservationRetry(
		context.Background(), "game/read-screen", 5, 0,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() (json.RawMessage, error) {
			attempts++
			return nil, want
		},
	)
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func TestSilentWGCObservationRetryReturnsOriginalFailureWhenExhausted(t *testing.T) {
	attempts := 0
	want := &Error{Code: "SCREEN_CAPTURE_FAILED", Stage: "brokering-observer-call", Cause: errors.New("WGC unavailable")}
	_, err := runWithSilentWGCObservationRetry(
		context.Background(), "game/read-screen", 3, 0,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() (json.RawMessage, error) {
			attempts++
			return nil, want
		},
	)
	if !errors.Is(err, want) || attempts != 3 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func TestSilentWGCObservationRetryHonorsCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	_, err := runWithSilentWGCObservationRetry(
		ctx, "game/read-screen", 5, time.Hour,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() (json.RawMessage, error) {
			attempts++
			cancel()
			return nil, &Error{Code: "SCREEN_CAPTURE_FAILED", Stage: "brokering-observer-call", Cause: errors.New("WGC unavailable")}
		},
	)
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}
