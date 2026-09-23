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

func TestCopyChangedFilePreservesLockedIdenticalCompanion(t *testing.T) {
	start := strings.Index(installerScript, "function Copy-ChangedFile(")
	end := strings.Index(installerScript, "function Wait-ExecutableExit(")
	if start < 0 || end <= start {
		t.Fatal("copy helper is missing")
	}
	root := t.TempDir()
	script := `$ErrorActionPreference = "Stop"
` + installerScript[start:end] + `
$source = Join-Path $env:WINDOWSAGENT_INSTALL_DATA_DIR "source.exe"
$destination = Join-Path $env:WINDOWSAGENT_INSTALL_DATA_DIR "running.exe"
[IO.File]::WriteAllText($source, "original")
[IO.File]::WriteAllText($destination, "original")
$lock = [IO.File]::Open($destination, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
try {
    Copy-ChangedFile -Source $source -Destination $destination
    [IO.File]::WriteAllText($source, "changed")
    $failed = $false
    try { Copy-ChangedFile -Source $source -Destination $destination } catch { $failed = $true }
    if (-not $failed) { throw "changed locked companion must fail explicitly" }
    if ([IO.File]::ReadAllText($destination) -cne "original") { throw "locked companion was modified" }
} finally { $lock.Dispose() }
Copy-ChangedFile -Source $source -Destination $destination
if ([IO.File]::ReadAllText($destination) -cne "changed") { throw "unlocked replacement failed" }
`
	if err := runInstaller(context.Background(), Request{DataDir: root}, script); err != nil {
		t.Fatal(err)
	}
}
