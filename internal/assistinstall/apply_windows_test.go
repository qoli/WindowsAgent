//go:build windows

package assistinstall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstallerPropagatesPowerShellFailure(t *testing.T) {
	root := t.TempDir()
	err := runInstaller(context.Background(), Request{
		Operation:   OperationRepair,
		StageDir:    filepath.Join(root, "stage"),
		CatalogPath: filepath.Join(root, "stage", "windowsagent-release.json"),
		DataDir:     filepath.Join(root, "data"),
	}, `$ErrorActionPreference = "Stop"
try {
    throw "intentional installer failure"
} catch {
    $failure = $_
    throw $failure
}`)
	if err == nil || !strings.Contains(err.Error(), "intentional installer failure") {
		t.Fatalf("runInstaller() error = %v", err)
	}
}
