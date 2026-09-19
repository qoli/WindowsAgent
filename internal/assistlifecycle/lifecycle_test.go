package assistlifecycle

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateDataDirRequiresCleanAbsolutePath(t *testing.T) {
	relative := "windows-agent"
	if err := validateDataDir(relative); err == nil {
		t.Fatal("relative data directory unexpectedly accepted")
	}
	abs := filepath.Join(string(filepath.Separator), "windows-agent")
	if runtime.GOOS == "windows" {
		abs = `C:\windows-agent`
	}
	if err := validateDataDir(abs); err != nil {
		t.Fatalf("clean absolute data directory rejected: %v", err)
	}
	dirty := abs + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(abs)
	if err := validateDataDir(dirty); err == nil {
		t.Fatal("unclean data directory unexpectedly accepted")
	}
}
