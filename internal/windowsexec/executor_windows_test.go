//go:build windows

package windowsexec

import (
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
