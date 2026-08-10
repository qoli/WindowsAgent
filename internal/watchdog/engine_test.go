package watchdog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeObserver struct {
	observation Observation
	err         error
	calls       int
}

func (f *fakeObserver) Observe(context.Context, Target) (Observation, error) {
	f.calls++
	return f.observation, f.err
}

type fakeRecoverer struct {
	calls int
	err   error
}

func (f *fakeRecoverer) RestartScheduledTask(context.Context, RecoveryConfig) error {
	f.calls++
	return f.err
}

type memoryStatusSink struct {
	statuses []Status
	err      error
}

func (s *memoryStatusSink) Write(status Status) error {
	if s.err != nil {
		return s.err
	}
	s.statuses = append(s.statuses, status)
	return nil
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestEngineUsesThresholdAndBoundedRecoveryWithoutSelfRecovery(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	observer := &fakeObserver{observation: Observation{Healthy: true, Detail: "ready"}}
	recoverer := &fakeRecoverer{}
	sink := &memoryStatusSink{}
	engine, err := NewEngine(config, observer, recoverer, sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	observer.observation = Observation{Healthy: false, Detail: "absent"}
	clock.now = clock.now.Add(time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 0 {
		t.Fatalf("recovery calls after first failure = %d", recoverer.calls)
	}
	clock.now = clock.now.Add(time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 1 {
		t.Fatalf("recovery calls after threshold = %d", recoverer.calls)
	}
	if got := engine.targets["event-stream"].status.State; got != StateGrace {
		t.Fatalf("state = %s", got)
	}
	// The engine owns no goroutine or process that can restart the watchdog.
	// A status-write failure exits the caller explicitly.
	sink.err = errors.New("disk unavailable")
	err = engine.Cycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "disk unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestEngineBootstrapsInDependencyOrderWithoutWaitingForRuntimeThreshold(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	capture := config.Targets[0]
	capture.ID = "capture-agent"
	capture.StartAfterHealthy = []string{"event-stream"}
	capture.Recovery.ScheduledTaskName = "Capture Agent"
	config.Targets = []Target{capture, config.Targets[0]}
	observer := &targetObserverFixture{health: map[string]bool{"event-stream": false, "capture-agent": false}}
	recoverer := &recordingRecoverer{}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recoverer.targets) != 1 || recoverer.targets[0] != "Agent Event Stream" {
		t.Fatalf("bootstrap recoveries = %v", recoverer.targets)
	}
	if got := engine.targets["capture-agent"].status.State; got != StateBlocked {
		t.Fatalf("capture state = %s", got)
	}
	observer.health["event-stream"] = true
	clock.now = clock.now.Add(6 * time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(recoverer.targets) != 2 || recoverer.targets[1] != "Capture Agent" {
		t.Fatalf("bootstrap recoveries = %v", recoverer.targets)
	}
}

type targetObserverFixture struct{ health map[string]bool }

func (f *targetObserverFixture) Observe(_ context.Context, target Target) (Observation, error) {
	return Observation{Healthy: f.health[target.ID], Detail: target.ID}, nil
}

type recordingRecoverer struct{ targets []string }

func (r *recordingRecoverer) RestartScheduledTask(_ context.Context, recovery RecoveryConfig) error {
	r.targets = append(r.targets, recovery.ScheduledTaskName)
	return nil
}

func TestEngineDoesNotRecoverWhenObservationItselfFails(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.Targets[0].FailureThreshold = 1
	observer := &fakeObserver{err: errors.New("process enumeration denied")}
	recoverer := &fakeRecoverer{}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 0 || engine.targets["event-stream"].status.State != StateObservationFailed {
		t.Fatalf("recovery calls=%d state=%s", recoverer.calls, engine.targets["event-stream"].status.State)
	}
}

func TestEngineOpensCircuitAfterConfiguredAttemptBudget(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.Targets[0].FailureThreshold = 1
	config.Targets[0].Recovery.MaxAttempts = 1
	config.Targets[0].Recovery.StartupGraceMS = 1
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "down"}}
	recoverer := &fakeRecoverer{err: errors.New("restart rejected")}
	engine, _ := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(2 * time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 1 || engine.targets["event-stream"].status.State != StateCircuitOpen {
		t.Fatalf("recovery calls=%d state=%s", recoverer.calls, engine.targets["event-stream"].status.State)
	}
}

func TestEngineLogsCircuitOpenOnlyOnceUntilStateChanges(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.Targets[0].FailureThreshold = 1
	config.Targets[0].Recovery.MaxAttempts = 1
	config.Targets[0].Recovery.StartupGraceMS = 1
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "down"}}
	recoverer := &fakeRecoverer{err: errors.New("restart rejected")}
	var logs bytes.Buffer
	engine, _ := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(&logs, nil)))
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		clock.now = clock.now.Add(time.Second)
		if err := engine.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Count(logs.String(), "watchdog_recovery_budget_exhausted"); got != 1 {
		t.Fatalf("budget-exhausted log count = %d\n%s", got, logs.String())
	}
}
