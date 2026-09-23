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

func TestAssistElevationMigrationIsTransactional(t *testing.T) {
	backup := strings.Index(installerScript, "$previousTaskXML[$entry.Key] = Export-ScheduledTask")
	migrate := strings.Index(installerScript, `Set-ScheduledTask -TaskName $agentTask -Principal $captureTask.Principal`)
	deploy := strings.Index(installerScript, `$deployOutput = & powershell.exe`)
	verify := strings.Index(installerScript, `[string]$installedTask.Principal.RunLevel -cne "Highest"`)
	commit := strings.Index(installerScript, `Remove-Item -LiteralPath $backupDir -Recurse -Force -ErrorAction SilentlyContinue`)
	readiness := strings.Index(installerScript, `$runtimeReadiness = Assert-AssistRuntimeReady`)
	if backup < 0 || migrate <= backup || deploy <= migrate || verify <= deploy || readiness <= verify || commit <= readiness {
		t.Fatal("elevation migration must retain rollback XML, precede restart, and verify before commit")
	}
	for _, required := range []string{
		`$captureTask.Principal.RunLevel = "Highest"`,
		`-StartupMode WatchdogManaged -AgentRunLevel Highest`,
		`[string]$installedTask.Principal.LogonType -cne "Interactive"`,
		`Register-ScheduledTask -TaskName $taskName -Xml $previousTaskXML[$taskName] -Force`,
	} {
		if !strings.Contains(installerScript, required) {
			t.Fatalf("Assist elevation contract missing %q", required)
		}
	}
	if strings.Contains(installerScript, "-AgentRunLevel Limited") {
		t.Fatal("Assist must not reinstall the Agent with a filtered token")
	}
}
