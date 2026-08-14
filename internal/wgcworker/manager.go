package wgcworker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/captureindicator"
)

const maxTransportAttempts = 5

type workerClient interface {
	PID() int
	Status(context.Context, time.Time) (capture.Status, error)
	Capture(context.Context, time.Time, capture.Request) (capture.Result, error)
	CaptureRegion(context.Context, time.Time, capture.RegionRequest) (capture.RegionResult, error)
	Close() error
}

type workerStarter func(context.Context, string, bool, *slog.Logger) (workerClient, error)

// Capturer is the Agent-side adapter for crash-isolated persistent WGC worker
// generations. An idempotent capture request may be replayed on a fresh
// generation after an explicit worker transport EOF. A region readback may
// additionally receive one fresh-generation recovery because it is an
// idempotent observation. The provider and capture request never change, and
// exhausted or non-transient failures remain explicit.
type Capturer struct {
	mu           sync.Mutex
	executable   string
	callTimeout  time.Duration
	logger       *slog.Logger
	trace        bool
	notifier     captureindicator.Notifier
	start        workerStarter
	client       workerClient
	generation   uint64
	closed       bool
	notifyFailed atomic.Bool
}

func New(ctx context.Context, executable string, callTimeout time.Duration, trace bool, notifier captureindicator.Notifier, logger *slog.Logger) (*Capturer, error) {
	return newWithStarter(ctx, executable, callTimeout, trace, notifier, logger, startWorkerProcess)
}

func newWithStarter(ctx context.Context, executable string, callTimeout time.Duration, trace bool, notifier captureindicator.Notifier, logger *slog.Logger, start workerStarter) (*Capturer, error) {
	if ctx == nil {
		return nil, errors.New("WGC worker initialization context is required")
	}
	if executable == "" || !filepath.IsAbs(executable) {
		return nil, errors.New("WGC worker executable must be an absolute path")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return nil, fmt.Errorf("stat WGC worker executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("WGC worker executable must be a regular file")
	}
	if callTimeout <= 0 || callTimeout > MaxCallDuration {
		return nil, fmt.Errorf("WGC worker call timeout must be from 1 through %s", MaxCallDuration)
	}
	if logger == nil {
		return nil, errors.New("WGC worker logger is required")
	}
	if notifier == nil {
		return nil, errors.New("capture activity notifier is required")
	}
	if start == nil {
		return nil, errors.New("WGC worker process starter is required")
	}
	capturer := &Capturer{executable: executable, callTimeout: callTimeout, trace: trace, notifier: notifier, logger: logger, start: start}
	deadline, err := effectiveInitializationDeadline(ctx)
	if err != nil {
		return nil, err
	}
	startContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if _, err := capturer.clientLocked(startContext); err != nil {
		return nil, err
	}
	return capturer, nil
}

func (c *Capturer) Status(ctx context.Context) (capture.Status, error) {
	deadline, err := effectiveDeadline(ctx, c.callTimeout)
	if err != nil {
		return capture.Status{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	client, err := c.clientLocked(ctx)
	if err != nil {
		return capture.Status{}, err
	}
	result, err := client.Status(ctx, deadline)
	if err != nil {
		c.retireLocked("status_failed", err)
		return capture.Status{}, fmt.Errorf("persistent WGC worker status: %w", err)
	}
	return result, nil
}

func (c *Capturer) Capture(ctx context.Context, request capture.Request) (capture.Result, error) {
	deadline, err := effectiveDeadline(ctx, c.callTimeout)
	if err != nil {
		return capture.Result{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyCapture("full")
	for attempt := 1; attempt <= maxTransportAttempts; attempt++ {
		client, err := c.clientLocked(ctx)
		if err != nil {
			return capture.Result{}, err
		}
		result, err := client.Capture(ctx, deadline, request)
		if err == nil {
			if attempt > 1 {
				c.logger.InfoContext(ctx, "wgc_worker_capture_retry_recovered",
					"capture_kind", "full", "attempts", attempt, "retry_count", attempt-1)
			}
			return result, nil
		}
		c.retireLocked("capture_failed", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return capture.Result{}, ctxErr
		}
		retryable := retryableTransportFailure(err)
		if attempt == maxTransportAttempts || !retryable {
			if retryable {
				c.logger.ErrorContext(ctx, "wgc_worker_capture_retry_exhausted",
					"capture_kind", "full", "attempts", attempt, "retry_count", attempt-1, "error", err)
			}
			return capture.Result{}, fmt.Errorf("persistent WGC worker capture: %w", err)
		}
		c.logger.WarnContext(ctx, "wgc_worker_capture_retry_scheduled",
			"capture_kind", "full",
			"failed_attempt", attempt,
			"next_attempt", attempt+1,
			"max_attempts", maxTransportAttempts,
			"error", err,
		)
	}
	return capture.Result{}, errors.New("unreachable persistent WGC worker capture retry state")
}

func (c *Capturer) CaptureRegion(ctx context.Context, request capture.RegionRequest) (capture.RegionResult, error) {
	deadline, err := effectiveDeadline(ctx, c.callTimeout)
	if err != nil {
		return capture.RegionResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyCapture("region")
	regionReadbackRecoveries := 0
	for attempt := 1; attempt <= maxTransportAttempts; attempt++ {
		client, err := c.clientLocked(ctx)
		if err != nil {
			return capture.RegionResult{}, err
		}
		result, err := client.CaptureRegion(ctx, deadline, request)
		if err == nil {
			if attempt > 1 {
				c.logger.InfoContext(ctx, "wgc_worker_capture_retry_recovered",
					"capture_kind", "region", "attempts", attempt, "retry_count", attempt-1)
			}
			return result, nil
		}
		c.retireLocked("region_failed", err)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return capture.RegionResult{}, ctxErr
		}
		retryable := retryableTransportFailure(err)
		if !retryable && regionReadbackRecoveries == 0 && retryableRegionReadbackFailure(err) {
			regionReadbackRecoveries++
			retryable = true
		}
		if attempt == maxTransportAttempts || !retryable {
			if retryable {
				c.logger.ErrorContext(ctx, "wgc_worker_capture_retry_exhausted",
					"capture_kind", "region", "attempts", attempt, "retry_count", attempt-1, "error", err)
			}
			return capture.RegionResult{}, fmt.Errorf("persistent WGC worker region capture: %w", err)
		}
		c.logger.WarnContext(ctx, "wgc_worker_capture_retry_scheduled",
			"capture_kind", "region",
			"failed_attempt", attempt,
			"next_attempt", attempt+1,
			"max_attempts", maxTransportAttempts,
			"error", err,
		)
	}
	return capture.RegionResult{}, errors.New("unreachable persistent WGC worker region retry state")
}

func retryableTransportFailure(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func retryableRegionReadbackFailure(err error) bool {
	var failure *capture.Error
	if !errors.As(err, &failure) {
		return false
	}
	return failure.Code == "capture_readback_failed"
}

func (c *Capturer) notifyCapture(kind string) {
	if err := c.notifier.Pulse(); err != nil && c.notifyFailed.CompareAndSwap(false, true) {
		c.logger.Warn("capture_indicator_pulse_failed", "capture_kind", kind, "error", err)
	}
}

func (c *Capturer) clientLocked(ctx context.Context) (workerClient, error) {
	if c.closed {
		return nil, errors.New("persistent WGC worker adapter is closed")
	}
	if c.client != nil {
		return c.client, nil
	}
	client, err := c.start(ctx, c.executable, c.trace, c.logger)
	if err != nil {
		return nil, fmt.Errorf("start persistent WGC worker: %w", err)
	}
	c.generation++
	c.client = client
	c.logger.Info("wgc_worker_started", "generation", c.generation, "process_id", client.PID(), "persistent", true)
	return client, nil
}

func (c *Capturer) retireLocked(reason string, cause error) {
	if c.client == nil {
		return
	}
	pid := c.client.PID()
	closeErr := c.client.Close()
	c.client = nil
	attributes := []any{"generation", c.generation, "process_id", pid, "reason", reason, "error", cause}
	if closeErr != nil {
		attributes = append(attributes, "close_error", closeErr)
	}
	c.logger.Warn("wgc_worker_retired", attributes...)
}

func (c *Capturer) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.client == nil {
		return nil
	}
	client := c.client
	c.client = nil
	err := client.Close()
	if err != nil {
		return fmt.Errorf("close persistent WGC worker: %w", err)
	}
	c.logger.Info("wgc_worker_stopped", "generation", c.generation, "process_id", client.PID())
	return nil
}
