package visuallog

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

const (
	RunIdle     = "idle"
	RunWarming  = "warming"
	RunActive   = "active"
	RunStopping = "stopping"
	RunStopped  = "stopped"
	RunFailed   = "failed"
)

var ErrRunActive = errors.New("visual log run is already active")
var ErrRunInactive = errors.New("visual log run is not active")

type RunStatus struct {
	State          string    `json:"state"`
	SessionID      string    `json:"sessionId,omitempty"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
	LastSequence   uint64    `json:"lastSequence"`
	DroppedSamples uint64    `json:"droppedSamples"`
	LastDropStage  string    `json:"lastDropStage,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type Controller struct {
	mu     sync.Mutex
	root   context.Context
	runner Runner
	status RunStatus
	cancel context.CancelFunc
	done   chan struct{}
	now    func() time.Time
}

func NewController(root context.Context, runner Runner) (*Controller, error) {
	if root == nil {
		return nil, errors.New("visual log controller context is required")
	}
	if err := runner.Validate(); err != nil {
		return nil, err
	}
	now := time.Now
	return &Controller{
		root: root, runner: runner, now: now,
		status: RunStatus{State: RunIdle, UpdatedAt: now().UTC()},
	}, nil
}

func (c *Controller) Start() (RunStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.State == RunWarming || c.status.State == RunActive || c.status.State == RunStopping {
		return c.status, ErrRunActive
	}
	sessionID, err := NewIdentity("vlogsession")
	if err != nil {
		return c.status, err
	}
	startedAt := c.now().UTC()
	runContext, cancel := context.WithCancel(c.root)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	c.status = RunStatus{State: RunWarming, SessionID: sessionID, StartedAt: startedAt, UpdatedAt: startedAt}
	runner := c.runner
	runner.SessionID = sessionID
	originalCommitted := runner.OnCommitted
	originalDropped := runner.OnDropped
	runner.OnWarmed = func() {
		c.update(func(status *RunStatus) {
			if status.State == RunWarming {
				status.State = RunActive
			}
		})
	}
	runner.OnCommitted = func(event eventstream.Event) {
		c.update(func(status *RunStatus) {
			if status.State != RunStopping {
				status.State = RunActive
			}
			status.LastSequence = event.Sequence
		})
		if originalCommitted != nil {
			originalCommitted(event)
		}
	}
	runner.OnDropped = func(sample DroppedSample) {
		c.update(func(status *RunStatus) {
			status.DroppedSamples++
			status.LastDropStage = sample.Stage
		})
		if originalDropped != nil {
			originalDropped(sample)
		}
	}
	go func() {
		defer close(done)
		err := runner.Run(runContext)
		c.update(func(status *RunStatus) {
			if err != nil {
				status.State = RunFailed
				status.Error = err.Error()
				return
			}
			status.State = RunStopped
		})
	}()
	return c.status, nil
}

func (c *Controller) Stop() (RunStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status.State != RunWarming && c.status.State != RunActive {
		return c.status, ErrRunInactive
	}
	c.status.State = RunStopping
	c.status.UpdatedAt = c.now().UTC()
	c.cancel()
	return c.status, nil
}

func (c *Controller) Status() RunStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Controller) Close() {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (c *Controller) update(change func(*RunStatus)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	change(&c.status)
	c.status.UpdatedAt = c.now().UTC()
}
