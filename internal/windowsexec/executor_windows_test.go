//go:build windows

package windowsexec

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveCommandSearchesPathAndReturnsAbsoluteExecutable(t *testing.T) {
	executable, argv, err := resolveCommand(Request{
		SchemaVersion: SchemaVersion,
		Operation:     OperationRun,
		Executable:    "cmd.exe",
		Argv:          []string{"/c", "exit", "0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(executable) {
		t.Fatalf("executable = %q, want absolute path", executable)
	}
	if len(argv) != 3 || argv[0] != "/c" || argv[2] != "0" {
		t.Fatalf("argv = %#v", argv)
	}
}

func TestOwnedExecutionTerminatesOnOutputLimit(t *testing.T) {
	_, err := (OSExecutor{}).Execute(context.Background(), Request{
		SchemaVersion:       SchemaVersion,
		Operation:           OperationRun,
		Executable:          "cmd.exe",
		Argv:                []string{"/d", "/s", "/c", "for /L %i in (1,1,1000000) do @echo 1234567890"},
		MaxOutputBytes:      1024,
		TimeoutMilliseconds: 30000,
	})
	var execErr *Error
	if !errors.As(err, &execErr) || execErr.Code != "EXEC_OUTPUT_LIMIT_EXCEEDED" || execErr.Stage != "capturing-process-output" {
		t.Fatalf("error = %v", err)
	}
	if execErr.PID == 0 || execErr.StdoutBytes <= 1024 || execErr.OutputLimitBytes != 1024 {
		t.Fatalf("output-limit evidence = %+v", execErr)
	}
}
