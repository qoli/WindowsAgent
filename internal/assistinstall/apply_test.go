package assistinstall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRejectsRelativePathsBeforeMutation(t *testing.T) {
	err := Apply(context.Background(), Request{StageDir: "relative", CatalogPath: "catalog.json", DataDir: "data"})
	if err == nil || !strings.Contains(err.Error(), "stage directory must be absolute") {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestApplyRequiresCatalogInsideStage(t *testing.T) {
	root := t.TempDir()
	err := Apply(context.Background(), Request{
		StageDir:    filepath.Join(root, "stage"),
		CatalogPath: filepath.Join(root, "elsewhere.json"),
		DataDir:     filepath.Join(root, "data"),
	})
	if err == nil || !strings.Contains(err.Error(), "inside the staging directory") {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestEmbeddedInstallerPreservesTaskOwnershipAndRollback(t *testing.T) {
	for _, required := range []string{
		"scheduled task '$($entry[0])' is not owned by WindowsAgent",
		"Export-ScheduledTask",
		"release artifact SHA-256 mismatch",
		"Register-ScheduledTask",
		"Wait-Health",
		"Confirm-ListenerOwner",
		"previousTaskXML",
		`$watchdogTask = "gameGuide Windows Watchdog"`,
		`$watchdogDescription = "gameGuide external process watchdog; no automatic self-recovery"`,
		"Stop-ScheduledTask -TaskName $watchdogTask",
		"Wait-ExecutableExit (Join-Path $binDir \"windows-watchdog.exe\")",
		"if ($watchdogWasRunning)",
		"windowsagent-release.json",
		"SHA256SUMS",
	} {
		if !strings.Contains(installerScript, required) {
			t.Fatalf("embedded installer is missing %q", required)
		}
	}
}
