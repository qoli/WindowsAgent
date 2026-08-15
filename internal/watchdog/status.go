package watchdog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

const (
	statusReplaceMaxAttempts = 40
	statusReplaceRetryDelay  = 25 * time.Millisecond
)

const (
	StatePending           = "PENDING"
	StateHealthy           = "HEALTHY"
	StateUnhealthy         = "UNHEALTHY"
	StateObservationFailed = "OBSERVATION_FAILED"
	StateRecovering        = "RECOVERING"
	StateRecoveryFailed    = "RECOVERY_FAILED"
	StateBackoff           = "BACKOFF"
	StateCircuitOpen       = "CIRCUIT_OPEN"
	StateGrace             = "STARTUP_GRACE"
	StateBlocked           = "BLOCKED"
	StateObserving         = "OBSERVING"
	StateIdle              = "IDLE"
)

type Status struct {
	SchemaVersion int            `json:"schemaVersion"`
	Watchdog      WatchdogStatus `json:"watchdog"`
	Targets       []TargetStatus `json:"targets"`
}

type WatchdogStatus struct {
	PID                  int        `json:"pid"`
	StartedAt            time.Time  `json:"startedAt"`
	State                string     `json:"state"`
	LastCycleStartedAt   time.Time  `json:"lastCycleStartedAt"`
	LastCycleCompletedAt *time.Time `json:"lastCycleCompletedAt,omitempty"`
}

type TargetStatus struct {
	ID                  string         `json:"id"`
	State               string         `json:"state"`
	Detail              string         `json:"detail,omitempty"`
	ConsecutiveFailures uint32         `json:"consecutiveFailures"`
	RestartAttempts     uint64         `json:"restartAttempts"`
	LastObservedAt      *time.Time     `json:"lastObservedAt,omitempty"`
	LastRecoveryAt      *time.Time     `json:"lastRecoveryAt,omitempty"`
	Observation         map[string]any `json:"observation,omitempty"`
}

type FileStatusSink struct {
	Name string
}

func (s FileStatusSink) Write(status Status) error {
	if s.Name == "" || !filepath.IsAbs(s.Name) {
		return fmt.Errorf("watchdog status path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(s.Name), 0o700); err != nil {
		return fmt.Errorf("create watchdog status directory: %w", err)
	}
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode watchdog status: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(s.Name), ".watchdog-status-*.tmp")
	if err != nil {
		return fmt.Errorf("create watchdog status staging file: %w", err)
	}
	temporaryName := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict watchdog status staging file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write watchdog status staging file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync watchdog status staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close watchdog status staging file: %w", err)
	}
	if err := replaceStatusFile(temporaryName, s.Name); err != nil {
		return fmt.Errorf("replace watchdog status: %w", err)
	}
	keep = true
	return nil
}

func replaceStatusFile(source, destination string) error {
	return retryStatusReplace(func() error {
		return os.Rename(source, destination)
	}, isRetryableStatusReplaceError, time.Sleep)
}

func retryStatusReplace(operation func() error, retryable func(error) bool, wait func(time.Duration)) error {
	for attempt := 1; attempt <= statusReplaceMaxAttempts; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		if !retryable(err) || attempt == statusReplaceMaxAttempts {
			return fmt.Errorf("atomic replace failed after %d attempt(s): %w", attempt, err)
		}
		wait(statusReplaceRetryDelay)
	}
	panic("unreachable status replace retry state")
}

func isRetryableStatusReplaceError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.Errno(32))
}

var osGetpid = os.Getpid
