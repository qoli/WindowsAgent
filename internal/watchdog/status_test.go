package watchdog

import (
	"encoding/json"
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
