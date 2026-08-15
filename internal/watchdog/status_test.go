package watchdog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStatusSinkAtomicallyPublishesStrictStatus(t *testing.T) {
	name := filepath.Join(t.TempDir(), "watchdog", "status.json")
	status := Status{SchemaVersion: 1, Watchdog: WatchdogStatus{
		PID: 12, StartedAt: time.Now().UTC(), State: StateIdle,
	}, Targets: []TargetStatus{{ID: "event-stream", State: StateHealthy}}}
	if err := (FileStatusSink{Name: name}).Write(status); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Status
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Watchdog.PID != 12 || decoded.Targets[0].State != StateHealthy {
		t.Fatalf("status = %+v", decoded)
	}
	staged, err := filepath.Glob(filepath.Join(filepath.Dir(name), ".watchdog-status-*.tmp"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("staging files = %v, error = %v", staged, err)
	}
}

func TestRetryStatusReplaceRetriesOnlyDeclaredTransientErrors(t *testing.T) {
	transient := errors.New("transient sharing violation")
	attempts := 0
	waits := 0
	err := retryStatusReplace(func() error {
		attempts++
		if attempts < 3 {
			return transient
		}
		return nil
	}, func(err error) bool {
		return errors.Is(err, transient)
	}, func(delay time.Duration) {
		waits++
		if delay != statusReplaceRetryDelay {
			t.Fatalf("retry delay = %s", delay)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || waits != 2 {
		t.Fatalf("attempts=%d waits=%d", attempts, waits)
	}
}

func TestRetryStatusReplacePreservesTerminalErrorWithoutRetry(t *testing.T) {
	terminal := errors.New("terminal replace failure")
	attempts := 0
	err := retryStatusReplace(func() error {
		attempts++
		return terminal
	}, func(error) bool { return false }, func(time.Duration) {
		t.Fatal("terminal failure must not wait")
	})
	if !errors.Is(err, terminal) || attempts != 1 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}

func TestRetryStatusReplaceFailsAfterBoundedAttempts(t *testing.T) {
	transient := errors.New("persistent sharing violation")
	attempts := 0
	err := retryStatusReplace(func() error {
		attempts++
		return transient
	}, func(error) bool { return true }, func(time.Duration) {})
	if !errors.Is(err, transient) || attempts != statusReplaceMaxAttempts {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
}
