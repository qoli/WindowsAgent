package visuallog

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

const (
	StateWarming = "warming"
	StateActive  = "active"
	StateStopped = "stopped"
	StateFailed  = "failed"
)

type Status struct {
	State          string    `json:"state"`
	SessionID      string    `json:"sessionId,omitempty"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
	LastSequence   uint64    `json:"lastSequence"`
	DroppedSamples uint64    `json:"droppedSamples"`
	LastDropStage  string    `json:"lastDropStage,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type Producer struct {
	mu     sync.Mutex
	root   context.Context
	runner Runner
	status Status
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	now    func() time.Time
}

func NewProducer(root context.Context, runner Runner) (*Producer, error) {
	if root == nil {
		return nil, errors.New("visual log producer context is required")
	}
	if err := runner.Validate(); err != nil {
		return nil, err
	}
	now := time.Now
	producer := &Producer{
		root: root, runner: runner, now: now,
	}
	producer.start()
	return producer, nil
}

func (c *Producer) start() {
	startedAt := c.now().UTC()
	runContext, cancel := context.WithCancel(c.root)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	c.status = Status{State: StateWarming, SessionID: c.runner.SessionID, StartedAt: startedAt, UpdatedAt: startedAt}
	runner := c.runner
	originalWarmed := runner.OnWarmed
	originalCommitted := runner.OnCommitted
	originalDropped := runner.OnDropped
	runner.OnWarmed = func() {
		c.update(func(status *Status) {
			if status.State == StateWarming {
				status.State = StateActive
			}
		})
		if originalWarmed != nil {
			originalWarmed()
		}
	}
	runner.OnCommitted = func(event eventstream.Event) {
		c.update(func(status *Status) {
			status.State = StateActive
			status.LastSequence = event.Sequence
		})
		if originalCommitted != nil {
			originalCommitted(event)
		}
	}
	runner.OnDropped = func(sample DroppedSample) {
		c.update(func(status *Status) {
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
		c.update(func(status *Status) {
			c.err = err
			if err != nil {
				status.State = StateFailed
				status.Error = err.Error()
				return
			}
			status.State = StateStopped
		})
	}()
}

func (c *Producer) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

func (c *Producer) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Producer) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Producer) Close() {
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

func (c *Producer) update(change func(*Status)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	change(&c.status)
	c.status.UpdatedAt = c.now().UTC()
}
