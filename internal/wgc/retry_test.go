package wgc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

func TestRetryTransientWGCCaptureRecoversFromInvalidItemSize(t *testing.T) {
	calls := 0
	retries := 0
	err := retryTransientWGCCapture(context.Background(), 3, 0, func() error {
		calls++
		if calls == 1 {
			return capture.Failure("desktop_unavailable", "primary monitor capture size is invalid", nil)
		}
		return nil
	}, func(attempt int, err error) {
		retries++
		if attempt != 1 || err == nil {
			t.Fatalf("attempt=%d error=%v", attempt, err)
		}
	})
	if err != nil || calls != 2 || retries != 1 {
		t.Fatalf("error=%v calls=%d retries=%d", err, calls, retries)
	}
}

func TestRetryTransientWGCCaptureDoesNotRetryPersistentDesktopFailure(t *testing.T) {
	calls := 0
	want := capture.Failure("desktop_unavailable", "primary monitor was not found", nil)
	err := retryTransientWGCCapture(context.Background(), 3, 0, func() error {
		calls++
		return want
	}, nil)
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestRetryTransientWGCCaptureExhaustionPreservesCaptureError(t *testing.T) {
	calls := 0
	want := capture.Failure("capture_session_failed", "failed to start WGC capture", errors.New("device removed"))
	err := retryTransientWGCCapture(context.Background(), 3, 0, func() error {
		calls++
		return want
	}, nil)
	var captureErr *capture.Error
	if calls != 3 || !errors.As(err, &captureErr) || captureErr.Code != "capture_session_failed" ||
		!strings.Contains(err.Error(), "after 3 attempts") || !strings.Contains(err.Error(), "device removed") {
		t.Fatalf("error=%v captureError=%#v calls=%d", err, captureErr, calls)
	}
}

func TestRetryTransientWGCCaptureRecoversFromRegionReadbackFailure(t *testing.T) {
	calls := 0
	err := retryTransientWGCCapture(context.Background(), 5, 0, func() error {
		calls++
		if calls <= 2 {
			return capture.Failure("capture_readback_failed", "failed to create the region unordered-access view", errors.New("HRESULT 0x80070057"))
		}
		return nil
	}, nil)
	if err != nil || calls != 3 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestRetryTransientWGCCaptureHonorsCancellationBetweenAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryTransientWGCCapture(ctx, 3, time.Hour, func() error {
		calls++
		return capture.Failure("capture_frame_failed", "WGC failed while retrieving a frame", nil)
	}, func(int, error) {
		cancel()
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}
