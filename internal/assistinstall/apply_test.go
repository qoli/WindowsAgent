package assistinstall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRejectsRelativePathsBeforeMutation(t *testing.T) {
	err := Apply(context.Background(), Request{Operation: OperationInstall, StageDir: "relative", CatalogPath: "catalog.json", DataDir: "data"})
	if err == nil || !strings.Contains(err.Error(), "stage directory must be absolute") {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestApplyRequiresCatalogInsideStage(t *testing.T) {
	root := t.TempDir()
	err := Apply(context.Background(), Request{
		Operation:   OperationInstall,
		StageDir:    filepath.Join(root, "stage"),
		CatalogPath: filepath.Join(root, "elsewhere.json"),
		DataDir:     filepath.Join(root, "data"),
	})
	if err == nil || !strings.Contains(err.Error(), "inside the staging directory") {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestApplyRejectsUnknownOperation(t *testing.T) {
	err := Apply(context.Background(), Request{Operation: "magic"})
	if err == nil || !strings.Contains(err.Error(), "unsupported setup operation") {
		t.Fatalf("Apply() error = %v", err)
	}
}

func TestConfigureWatchdogRejectsRelativeDataDirectory(t *testing.T) {
	err := ConfigureWatchdog(context.Background(), "relative", true)
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("ConfigureWatchdog() error = %v", err)
	}
}

func TestEmbeddedInstallerWrapsRepositoryInstallersAndRollback(t *testing.T) {
	for _, required := range []string{
		"scheduled task '$($entry.Key)' is not owned by WindowsAgent",
		"Export-ScheduledTask",
		"release artifact SHA-256 mismatch",
		"Register-ScheduledTask",
		"previousTaskXML",
		`install-windows-capture-agent.ps1`,
		`-AllowEmptyRules`,
		`-StartupMode WatchdogManaged`,
		`install-windows-watchdog.ps1`,
		`-StartAtLogon:$watchdogStartAtLogon`,
		`deploy-windows-binaries.ps1`,
		`-TimeoutSeconds 180`,
		`task_actions_preserved`,
		`WindowsAgent installation is incomplete; use Repair`,
		"windowsagent-release.json",
		"SHA256SUMS",
	} {
		if !strings.Contains(installerScript, required) {
			t.Fatalf("embedded installer is missing %q", required)
		}
	}
}
