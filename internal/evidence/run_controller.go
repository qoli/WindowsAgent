package evidence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

const (
	DefaultRunDurationSeconds uint32 = 20 * 60
	MaxRunDurationSeconds     uint32 = 20 * 60
	maxRetainedRunStatuses           = 64

	RunIdle      = "idle"
	RunStarting  = "starting"
	RunRecording = "recording"
	RunCompleted = "completed"
	RunFailed    = "failed"
)

var (
	ErrRunActive       = errors.New("evidence recording run is already active")
	ErrRunNotFound     = errors.New("evidence recording run was not found")
	ErrDurationInvalid = errors.New("evidence recording durationSeconds must be an integer between 1 and 1200")
)

type RunRequest struct {
	DurationSeconds *uint32 `json:"durationSeconds,omitempty"`
}

type RunStatus struct {
	State                  string     `json:"state"`
	RunID                  string     `json:"runId,omitempty"`
	Finite                 bool       `json:"finite"`
	DefaultDurationSeconds uint32     `json:"defaultDurationSeconds"`
	MaxDurationSeconds     uint32     `json:"maxDurationSeconds"`
	DurationSeconds        uint32     `json:"durationSeconds,omitempty"`
	RequestedAt            *time.Time `json:"requestedAt,omitempty"`
	StartedAt              *time.Time `json:"startedAt,omitempty"`
	EndsAt                 *time.Time `json:"endsAt,omitempty"`
	CompletedAt            *time.Time `json:"completedAt,omitempty"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	LastScheduledAt        *time.Time `json:"lastScheduledAt,omitempty"`
	Frames                 uint64     `json:"frames"`
	Gaps                   uint64     `json:"gaps"`
	TapFailures            uint64     `json:"tapFailures"`
	LastError              string     `json:"lastError,omitempty"`
	LastTapError           string     `json:"lastTapError,omitempty"`
}

type RunController struct {
	mu       sync.Mutex
	root     context.Context
	recorder Recorder
	now      func() time.Time
	newID    func() (string, error)
	current  string
	latest   RunStatus
	runs     map[string]RunStatus
	order    []string
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewRunController(root context.Context, recorder Recorder) (*RunController, error) {
	if root == nil {
		return nil, errors.New("evidence run controller context is required")
	}
	if err := recorder.Validate(); err != nil {
		return nil, err
	}
	now := time.Now
	return &RunController{
		root: root, recorder: recorder, now: now, newID: newRunIdentity,
		latest: baseRunStatus(RunIdle, now().UTC()), runs: make(map[string]RunStatus),
	}, nil
}

func (c *RunController) Start(request RunRequest) (RunStatus, error) {
	durationSeconds, err := resolveRunDuration(request)
	if err != nil {
		return c.Status(), err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root.Err() != nil {
		return c.latest, errors.New("evidence run controller is closed")
	}
	if c.latest.State == RunStarting || c.latest.State == RunRecording {
		return c.latest, ErrRunActive
	}
	runID, err := c.newID()
	if err != nil {
		return c.latest, err
	}
	requestedAt := c.now().UTC()
	endsAt := requestedAt.Add(time.Duration(durationSeconds) * time.Second)
	runContext, cancel := context.WithDeadline(c.root, endsAt)
	done := make(chan struct{})
	status := baseRunStatus(RunStarting, requestedAt)
	status.RunID = runID
	status.DurationSeconds = durationSeconds
	status.RequestedAt = timePointer(requestedAt)
	status.EndsAt = timePointer(endsAt)
	c.current, c.latest, c.cancel, c.done = runID, status, cancel, done
	c.rememberLocked(status)

	recorder := c.recorder
	baseLifecycle := recorder.Lifecycle
	recorder.Lifecycle = &runLifecycle{
		delegate: baseLifecycle,
		started: func() {
			c.update(runID, func(current *RunStatus) {
				current.State = RunRecording
				current.StartedAt = timePointer(c.now().UTC())
			})
		},
	}
	baseCommitted := recorder.OnCommitted
	recorder.OnCommitted = func(record Record) {
		c.update(runID, func(current *RunStatus) {
			current.LastScheduledAt = timePointer(record.ScheduledAt)
			if record.Kind == "frame" {
				current.Frames++
			} else {
				current.Gaps++
				if record.Gap != nil {
					current.LastError = record.Gap.Error
				}
			}
		})
		if baseCommitted != nil {
			baseCommitted(record)
		}
	}
	baseTapFailed := recorder.OnTapFailed
	recorder.OnTapFailed = func(cause error) {
		c.update(runID, func(current *RunStatus) {
			current.TapFailures++
			current.LastTapError = cause.Error()
		})
		if baseTapFailed != nil {
			baseTapFailed(cause)
		}
	}

	go func() {
		defer close(done)
		runErr := recorder.Run(runContext)
		cancel()
		c.update(runID, func(current *RunStatus) {
			current.CompletedAt = timePointer(c.now().UTC())
			if runErr != nil {
				current.State = RunFailed
				current.LastError = runErr.Error()
				return
			}
			current.State = RunCompleted
		})
	}()
	return status, nil
}

func (c *RunController) Status() RunStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latest
}

func (c *RunController) RunStatus(runID string) (RunStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status, ok := c.runs[runID]
	if !ok {
		return RunStatus{}, ErrRunNotFound
	}
	return status, nil
}

func (c *RunController) Close() {
	c.mu.Lock()
	c.cancelCurrentLocked()
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (c *RunController) update(runID string, change func(*RunStatus)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status, ok := c.runs[runID]
	if !ok {
		return
	}
	change(&status)
	status.UpdatedAt = c.now().UTC()
	c.runs[runID] = status
	if c.current == runID {
		c.latest = status
	}
}

func (c *RunController) rememberLocked(status RunStatus) {
	c.runs[status.RunID] = status
	c.order = append(c.order, status.RunID)
	if len(c.order) <= maxRetainedRunStatuses {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.runs, oldest)
}

func (c *RunController) cancelCurrentLocked() {
	if c.cancel != nil {
		c.cancel()
	}
}

func resolveRunDuration(request RunRequest) (uint32, error) {
	if request.DurationSeconds == nil {
		return DefaultRunDurationSeconds, nil
	}
	if *request.DurationSeconds < 1 || *request.DurationSeconds > MaxRunDurationSeconds {
		return 0, ErrDurationInvalid
	}
	return *request.DurationSeconds, nil
}

func baseRunStatus(state string, at time.Time) RunStatus {
	return RunStatus{
		State: state, Finite: true, DefaultDurationSeconds: DefaultRunDurationSeconds,
		MaxDurationSeconds: MaxRunDurationSeconds, UpdatedAt: at,
	}
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

func newRunIdentity() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create evidence run identity: %w", err)
	}
	return "evr_" + hex.EncodeToString(random), nil
}

type runLifecycle struct {
	delegate videocapture.Lifecycle
	started  func()
}

func (l *runLifecycle) Start() error {
	if err := l.delegate.Start(); err != nil {
		return err
	}
	l.started()
	return nil
}

func (l *runLifecycle) Stop() { l.delegate.Stop() }
