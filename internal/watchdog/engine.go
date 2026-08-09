package watchdog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

type Observation struct {
	Healthy bool
	Detail  string
	Data    map[string]any
}

type Observer interface {
	Observe(context.Context, Target) (Observation, error)
}

type Recoverer interface {
	RestartScheduledTask(context.Context, RecoveryConfig) error
}

type StatusSink interface {
	Write(Status) error
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type Engine struct {
	config    Config
	observer  Observer
	recoverer Recoverer
	sink      StatusSink
	logger    *slog.Logger
	clock     Clock
	startedAt time.Time
	ordered   []Target
	targets   map[string]*targetRuntime
}

type targetRuntime struct {
	status            TargetStatus
	restartAttempts   []time.Time
	nextObservationAt time.Time
	bootstrapped      bool
}

func NewEngine(config Config, observer Observer, recoverer Recoverer, sink StatusSink, logger *slog.Logger) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if observer == nil || recoverer == nil || sink == nil || logger == nil {
		return nil, errors.New("watchdog observer, recoverer, status sink, and logger are required")
	}
	ordered, err := config.StartupOrder()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	engine := &Engine{
		config: config, observer: observer, recoverer: recoverer, sink: sink, logger: logger,
		clock: realClock{}, startedAt: now, ordered: ordered, targets: make(map[string]*targetRuntime, len(config.Targets)),
	}
	for _, target := range config.Targets {
		engine.targets[target.ID] = &targetRuntime{status: TargetStatus{ID: target.ID, State: StatePending}}
	}
	return engine, nil
}

func (e *Engine) Run(ctx context.Context) error {
	if err := e.Cycle(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(e.config.CheckIntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.Cycle(ctx); err != nil {
				return err
			}
		}
	}
}

func (e *Engine) Cycle(ctx context.Context) error {
	cycleStarted := e.clock.Now()
	status := e.snapshot(StateObserving, cycleStarted, time.Time{})
	if err := e.sink.Write(status); err != nil {
		return fmt.Errorf("write watchdog observing state: %w", err)
	}
	for _, target := range e.ordered {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := e.observeTarget(ctx, target, cycleStarted); err != nil {
			return err
		}
		if err := e.sink.Write(e.snapshot(StateObserving, cycleStarted, time.Time{})); err != nil {
			return fmt.Errorf("write watchdog target state: %w", err)
		}
	}
	completed := e.clock.Now()
	if err := e.sink.Write(e.snapshot(StateIdle, cycleStarted, completed)); err != nil {
		return fmt.Errorf("write watchdog completed state: %w", err)
	}
	return nil
}

func (e *Engine) observeTarget(ctx context.Context, target Target, cycleStarted time.Time) error {
	runtime := e.targets[target.ID]
	now := e.clock.Now()
	if !runtime.bootstrapped {
		var blocked []string
		for _, dependency := range target.StartAfterHealthy {
			if e.targets[dependency].status.State != StateHealthy {
				blocked = append(blocked, dependency)
			}
		}
		if len(blocked) != 0 {
			runtime.status.State = StateBlocked
			runtime.status.Detail = "waiting for healthy startup targets: " + strings.Join(blocked, ", ")
			return nil
		}
	}
	if now.Before(runtime.nextObservationAt) {
		runtime.status.State = StateGrace
		runtime.status.Detail = "waiting for configured startup grace period"
		return nil
	}
	observation, err := e.observer.Observe(ctx, target)
	runtime.status.LastObservedAt = timePointer(now)
	if err != nil {
		runtime.status.State = StateObservationFailed
		runtime.status.Detail = err.Error()
		runtime.status.Observation = nil
		e.logger.Error("watchdog_observation_failed", "target_id", target.ID, "error", err)
		return nil
	}
	runtime.status.Observation = observation.Data
	if observation.Healthy {
		if runtime.status.State != StateHealthy {
			e.logger.Info("watchdog_target_healthy", "target_id", target.ID, "detail", observation.Detail)
		}
		runtime.status.State = StateHealthy
		runtime.status.Detail = observation.Detail
		runtime.status.ConsecutiveFailures = 0
		runtime.bootstrapped = true
		return nil
	}
	runtime.status.ConsecutiveFailures++
	runtime.status.State = StateUnhealthy
	runtime.status.Detail = observation.Detail
	threshold := target.FailureThreshold
	if !runtime.bootstrapped {
		threshold = 1
	}
	e.logger.Warn("watchdog_target_unhealthy", "target_id", target.ID, "detail", observation.Detail,
		"consecutive_failures", runtime.status.ConsecutiveFailures, "failure_threshold", threshold,
		"bootstrap", !runtime.bootstrapped)
	if runtime.status.ConsecutiveFailures < threshold {
		return nil
	}

	windowStart := now.Add(-time.Duration(target.Recovery.AttemptWindowMS) * time.Millisecond)
	attempts := runtime.restartAttempts[:0]
	for _, attempt := range runtime.restartAttempts {
		if !attempt.Before(windowStart) {
			attempts = append(attempts, attempt)
		}
	}
	runtime.restartAttempts = attempts
	if len(attempts) >= int(target.Recovery.MaxAttempts) {
		runtime.status.State = StateCircuitOpen
		runtime.status.Detail = "configured recovery attempt budget exhausted"
		e.logger.Error("watchdog_recovery_budget_exhausted", "target_id", target.ID,
			"attempts", len(attempts), "window_ms", target.Recovery.AttemptWindowMS)
		return nil
	}
	if len(attempts) > 0 {
		nextAttempt := attempts[len(attempts)-1].Add(time.Duration(target.Recovery.BackoffMS) * time.Millisecond)
		if now.Before(nextAttempt) {
			runtime.status.State = StateBackoff
			runtime.status.Detail = "waiting for configured recovery backoff"
			return nil
		}
	}

	recoveryContext, cancel := context.WithTimeout(ctx, time.Duration(target.Recovery.ActionTimeoutMS)*time.Millisecond)
	defer cancel()
	runtime.status.State = StateRecovering
	runtime.status.Detail = "restarting exact configured Scheduled Task"
	if err := e.sink.Write(e.snapshot(StateObserving, cycleStarted, time.Time{})); err != nil {
		return fmt.Errorf("write watchdog recovery state: %w", err)
	}
	e.logger.Warn("watchdog_recovery_started", "target_id", target.ID,
		"scheduled_task", target.Recovery.ScheduledTaskName)
	if err := e.recoverer.RestartScheduledTask(recoveryContext, target.Recovery); err != nil {
		runtime.restartAttempts = append(runtime.restartAttempts, now)
		runtime.status.RestartAttempts++
		runtime.status.LastRecoveryAt = timePointer(now)
		runtime.status.State = StateRecoveryFailed
		runtime.status.Detail = err.Error()
		e.logger.Error("watchdog_recovery_failed", "target_id", target.ID, "error", err)
		return nil
	}
	runtime.restartAttempts = append(runtime.restartAttempts, now)
	runtime.status.RestartAttempts++
	runtime.status.LastRecoveryAt = timePointer(now)
	runtime.status.State = StateGrace
	runtime.status.Detail = "scheduled task restart completed; waiting for startup grace period"
	runtime.status.ConsecutiveFailures = 0
	runtime.nextObservationAt = now.Add(time.Duration(target.Recovery.StartupGraceMS) * time.Millisecond)
	e.logger.Info("watchdog_recovery_completed", "target_id", target.ID,
		"scheduled_task", target.Recovery.ScheduledTaskName, "startup_grace_ms", target.Recovery.StartupGraceMS)
	return nil
}

func (e *Engine) snapshot(state string, cycleStarted, cycleCompleted time.Time) Status {
	targets := make([]TargetStatus, 0, len(e.targets))
	for _, runtime := range e.targets {
		targets = append(targets, runtime.status)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return Status{
		SchemaVersion: SchemaVersion,
		Watchdog: WatchdogStatus{
			PID: osGetpid(), StartedAt: e.startedAt, State: state,
			LastCycleStartedAt: cycleStarted, LastCycleCompletedAt: optionalTimePointer(cycleCompleted),
		},
		Targets: targets,
	}
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

func optionalTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return timePointer(value)
}
