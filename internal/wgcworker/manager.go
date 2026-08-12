package wgcworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

type workerClient interface {
	PID() int
	Status(context.Context, time.Time) (capture.Status, error)
	Capture(context.Context, time.Time, capture.Request) (capture.Result, error)
	CaptureRegion(context.Context, time.Time, capture.RegionRequest) (capture.RegionResult, error)
	Close() error
}

type workerStarter func(string, bool, *slog.Logger) (workerClient, error)

// Capturer is the Agent-side adapter for one crash-isolated persistent WGC
// worker generation. A failed call retires that generation without replaying
// the request. A later independent call may start a new generation.
type Capturer struct {
	mu          sync.Mutex
	executable  string
	callTimeout time.Duration
	logger      *slog.Logger
	trace       bool
	start       workerStarter
	client      workerClient
	generation  uint64
	closed      bool
}

func New(executable string, callTimeout time.Duration, trace bool, logger *slog.Logger) (*Capturer, error) {
	return newWithStarter(executable, callTimeout, trace, logger, startWorkerProcess)
}

func newWithStarter(executable string, callTimeout time.Duration, trace bool, logger *slog.Logger, start workerStarter) (*Capturer, error) {
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
	if start == nil {
		return nil, errors.New("WGC worker process starter is required")
	}
	return &Capturer{executable: executable, callTimeout: callTimeout, trace: trace, logger: logger, start: start}, nil
}

func (c *Capturer) Status(ctx context.Context) (capture.Status, error) {
	deadline, err := effectiveDeadline(ctx, c.callTimeout)
	if err != nil {
		return capture.Status{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	client, err := c.clientLocked()
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
	client, err := c.clientLocked()
	if err != nil {
		return capture.Result{}, err
	}
	result, err := client.Capture(ctx, deadline, request)
	if err != nil {
		c.retireLocked("capture_failed", err)
		return capture.Result{}, fmt.Errorf("persistent WGC worker capture: %w", err)
	}
	return result, nil
}

func (c *Capturer) CaptureRegion(ctx context.Context, request capture.RegionRequest) (capture.RegionResult, error) {
	deadline, err := effectiveDeadline(ctx, c.callTimeout)
	if err != nil {
		return capture.RegionResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	client, err := c.clientLocked()
	if err != nil {
		return capture.RegionResult{}, err
	}
	result, err := client.CaptureRegion(ctx, deadline, request)
	if err != nil {
		c.retireLocked("region_failed", err)
		return capture.RegionResult{}, fmt.Errorf("persistent WGC worker region capture: %w", err)
	}
	return result, nil
}

func (c *Capturer) clientLocked() (workerClient, error) {
	if c.closed {
		return nil, errors.New("persistent WGC worker adapter is closed")
	}
	if c.client != nil {
		return c.client, nil
	}
	client, err := c.start(c.executable, c.trace, c.logger)
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
