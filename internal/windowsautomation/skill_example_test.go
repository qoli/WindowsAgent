package windowsautomation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the shipped authoring asset through the real loader and runner.
// This is portable fixture validation, not live Windows acceptance.
func TestExperimentalSkillReadFileExample(t *testing.T) {
	pkg, err := LoadDirectory(filepath.Join("..", "..", ".agents", "skills", "use-windows-starlark", "assets", "read-file"))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(&recordingProcessHost{}, OSFileHost{})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, "sample.txt")
	if err := os.WriteFile(filename, []byte("A雪🙂"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter := &recordingReporter{}
	output, err := runner.Run(context.Background(), pkg, map[string]any{"path": filename}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Verified   bool `json:"verified"`
		Characters int  `json:"characters"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Verified || result.Characters != 3 {
		t.Fatalf("unexpected result: %s", output)
	}
	if len(reporter.events) != 1 || reporter.events[0].kind != ActivityEventType {
		t.Fatalf("missing durable activity: %+v", reporter.events)
	}
	for _, tc := range []struct{ name, path, code string }{
		{"directory", directory, "EXPECTED_FILE"},
		{"missing", filepath.Join(directory, "missing.txt"), "FILESYSTEM_OPERATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runner.Run(context.Background(), pkg, map[string]any{"path": tc.path}, &recordingReporter{})
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
		})
	}
}
