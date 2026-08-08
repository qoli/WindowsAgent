package wgc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

const (
	wgcCaptureAttempts = 3
	wgcRetryDelay      = 25 * time.Millisecond
)

func retryTransientWGCCapture(
	ctx context.Context,
	attempts int,
	delay time.Duration,
	operation func() error,
	onRetry func(attempt int, err error),
) error {
	if attempts <= 0 {
		return errors.New("WGC capture retry attempts must be positive")
	}
	if operation == nil {
		return errors.New("WGC capture retry operation is required")
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation()
		if err == nil {
			return nil
		}
		if !isTransientWGCError(err) {
			return err
		}
		if attempt == attempts {
			return fmt.Errorf("WGC transient capture failed after %d attempts: %w", attempts, err)
		}
		if onRetry != nil {
			onRetry(attempt, err)
		}
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("WGC capture retry exhausted without an error")
}

func isTransientWGCError(err error) bool {
	var captureErr *capture.Error
	if !errors.As(err, &captureErr) {
		return false
	}
	switch captureErr.Code {
	case "capture_frame_failed", "capture_session_failed", "capture_size_mismatch":
		return true
	case "desktop_unavailable":
		return captureErr.Message == "primary monitor capture size is invalid"
	default:
		return false
	}
}
