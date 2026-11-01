package watchdog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeObserver struct {
	observation Observation
	err         error
	calls       int
	onObserve   func()
}

func (f *fakeObserver) Observe(context.Context, Target) (Observation, error) {
	f.calls++
	if f.onObserve != nil {
		f.onObserve()
	}
	return f.observation, f.err
}

func TestEnginePreservesProbeCauseAndRecoveryTimingInLogs(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	observer := &fakeObserver{
		observation: Observation{Healthy: false, Detail: "health request failed", Data: map[string]any{
			"probe0": map[string]any{"pid": 42, "sessionId": 1},
			"probe1": map[string]any{"error": "context deadline exceeded", "timeoutMs": 1500},
		}},
		onObserve: func() { clock.now = clock.now.Add(1500 * time.Millisecond) },
	}
	recoverer := &fakeRecoverer{onRestart: func() { clock.now = clock.now.Add(17 * time.Second) }}
	var logs bytes.Buffer
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := engine.targets["event-stream"].status
	if status.LastObservationDurationMS != 1500 || status.LastRecoveryDurationMS != 17000 {
		t.Fatalf("timing evidence=%+v", status)
	}
	records := map[string]map[string]any{}
	decoder := json.NewDecoder(&logs)
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		records[record["msg"].(string)] = record
	}
	unhealthy := records["watchdog_target_unhealthy"]
	if unhealthy["duration_ms"] != float64(1500) {
		t.Fatalf("unhealthy=%v", unhealthy)
	}
	evidence := unhealthy["observation"].(map[string]any)
	if evidence["probe0"].(map[string]any)["pid"] != float64(42) || evidence["probe1"].(map[string]any)["error"] != "context deadline exceeded" {
		t.Fatalf("probe evidence=%v", evidence)
	}
	completed := records["watchdog_recovery_completed"]
	if completed["duration_ms"] != float64(17000) || completed["startup_deadline"] == nil {
		t.Fatalf("recovery=%v", completed)
	}
}

type fakeRecoverer struct {
	calls     int
	err       error
	onRestart func()
}

func (f *fakeRecoverer) RestartScheduledTask(context.Context, RecoveryConfig) error {
	f.calls++
	if f.onRestart != nil {
		f.onRestart()
	}
	return f.err
}

func TestEngineStartupGraceBeginsAfterSlowRecovery(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigJSON()))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "absent"}}
	recoverer := &fakeRecoverer{onRestart: func() { clock.now = clock.now.Add(17 * time.Second) }}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	recoveredAt := clock.now
	for index, elapsed := range []time.Duration{0, 5*time.Second - time.Millisecond} {
		clock.now = recoveredAt.Add(elapsed)
		if err := engine.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
		if observer.calls != index+2 || recoverer.calls != 1 || engine.targets["event-stream"].status.State != StateGrace {
			t.Fatalf("after recovery + %s: observations=%d restarts=%d state=%s", elapsed, observer.calls, recoverer.calls, engine.targets["event-stream"].status.State)
		}
	}
	observer.observation = Observation{Healthy: true, Detail: "ready"}
	clock.now = recoveredAt.Add(5 * time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if observer.calls != 4 || recoverer.calls != 1 || engine.targets["event-stream"].status.State != StateHealthy {
		t.Fatalf("after startup grace: observations=%d restarts=%d state=%s", observer.calls, recoverer.calls, engine.targets["event-stream"].status.State)
	}
}

func TestEngineAllowsEarlyStartupReadiness(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "not listening yet"}}
	recoverer := &fakeRecoverer{}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	observer.observation = Observation{Healthy: true, Detail: "ready"}
	clock.now = clock.now.Add(time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 1 || observer.calls != 2 || engine.targets["event-stream"].status.State != StateHealthy {
		t.Fatalf("early readiness: restarts=%d observations=%d state=%s", recoverer.calls, observer.calls, engine.targets["event-stream"].status.State)
	}
	// Once readiness is confirmed, normal failure detection resumes immediately.
	observer.observation = Observation{Healthy: false, Detail: "stopped"}
	for range 2 {
		clock.now = clock.now.Add(time.Second)
		if err := engine.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if recoverer.calls != 2 {
		t.Fatalf("runtime failure restarts=%d", recoverer.calls)
	}
}

func TestEngineProtectsSlowStartupUntilConfiguredDeadline(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	config.Targets[0].Recovery.MaxAttempts = 1
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "still initializing"}}
	recoverer := &fakeRecoverer{}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := clock.now
	for _, elapsed := range []time.Duration{time.Second, 5*time.Second - time.Millisecond} {
		clock.now = started.Add(elapsed)
		if err := engine.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
		status := engine.targets["event-stream"].status
		if recoverer.calls != 1 || status.State != StateGrace || status.ConsecutiveFailures != 0 {
			t.Fatalf("startup at %s: restarts=%d status=%+v", elapsed, recoverer.calls, status)
		}
	}
	if observer.calls != 3 {
		t.Fatalf("startup observations=%d", observer.calls)
	}
	clock.now = started.Add(5 * time.Second)
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 1 || engine.targets["event-stream"].status.State != StateCircuitOpen {
		t.Fatalf("expired startup budget: restarts=%d state=%s", recoverer.calls, engine.targets["event-stream"].status.State)
	}
}

func TestEngineStartupObservationFailureRemainsExplicit(t *testing.T) {
	config, _ := ParseConfig([]byte(validConfigJSON()))
	observer := &fakeObserver{observation: Observation{Healthy: false, Detail: "absent"}}
	recoverer := &fakeRecoverer{}
	engine, err := NewEngine(config, observer, recoverer, &memoryStatusSink{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)}
	engine.clock = clock
	if err := engine.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := engine.targets["event-stream"].status.StartupDeadline
	if deadline == nil {
		t.Fatal("startup deadline is not published")
	}
	observer.err = errors.New("process enumeration denied")
	for _, elapsed := range []time.Duration{time.Second, 6 * time.Second} {
		clock.now = deadline.Add(elapsed - 5*time.Second)
		if err := engine.Cycle(context.Background()); err != nil {
			t.Fatal(err)
		}
		status := engine.targets["event-stream"].status
		if recoverer.calls != 1 || status.State != StateObservationFailed || !strings.Contains(status.Detail, observer.err.Error()) || !status.StartupDeadline.Equal(*deadline) {
			t.Fatalf("unverifiable startup: restarts=%d status=%+v", recoverer.calls, status)
		}
	}
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

func TestEngineRechecksDependenciesBeforeEveryRecovery(t *testing.T) {
	for _, test := range []struct {
		name             string
		dependentHealthy bool
		dependencyError  error
		wantState        string
		wantRecoveries   []string
	}{
		{name: "unhealthy dependent waits", wantState: StateBlocked, wantRecoveries: []string{"Agent Event Stream"}},
		{name: "healthy dependent remains independent", dependentHealthy: true, wantState: StateHealthy, wantRecoveries: []string{"Agent Event Stream"}},
		{name: "unverifiable dependency blocks recovery", dependencyError: errors.New("process enumeration denied"), wantState: StateBlocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := ParseConfig([]byte(validConfigJSON()))
			if err != nil {
				t.Fatal(err)
			}
			config.Targets[0].FailureThreshold = 1
			capture := config.Targets[0]
			capture.ID = "capture-agent"
			capture.StartAfterHealthy = []string{"event-stream"}
			capture.Recovery.ScheduledTaskName = "Capture Agent"
			config.Targets = append(config.Targets, capture)
			observer := &targetObserverFixture{health: map[string]bool{"event-stream": true, "capture-agent": true}, errors: map[string]error{}, calls: map[string]int{}}
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
			observer.health["event-stream"] = false
			observer.health["capture-agent"] = test.dependentHealthy
			observer.errors["event-stream"] = test.dependencyError
			clock.now = clock.now.Add(time.Second)
			if err := engine.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			dependent := engine.targets["capture-agent"]
			if !slices.Equal(recoverer.targets, test.wantRecoveries) || dependent.status.State != test.wantState {
				t.Fatalf("recoveries=%v state=%s", recoverer.targets, dependent.status.State)
			}
			if dependent.status.RestartAttempts != 0 || len(dependent.restartAttempts) != 0 || observer.calls["capture-agent"] != 2 {
				t.Fatalf("dependent attempts=%d observations=%d", dependent.status.RestartAttempts, observer.calls["capture-agent"])
			}
			observer.health["event-stream"] = true
			delete(observer.errors, "event-stream")
			clock.now = clock.now.Add(6 * time.Second)
			if err := engine.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(test.wantRecoveries)
			if !test.dependentHealthy {
				want = append(want, "Capture Agent")
			}
			if !slices.Equal(recoverer.targets, want) {
				t.Fatalf("after dependency recovers: recoveries=%v want=%v", recoverer.targets, want)
			}
		})
	}
}

type targetObserverFixture struct {
	health map[string]bool
	errors map[string]error
	calls  map[string]int
}

func (f *targetObserverFixture) Observe(_ context.Context, target Target) (Observation, error) {
	if f.calls != nil {
		f.calls[target.ID]++
	}
	return Observation{Healthy: f.health[target.ID], Detail: target.ID}, f.errors[target.ID]
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
