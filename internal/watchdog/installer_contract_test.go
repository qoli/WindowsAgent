package watchdog

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerExplicitlyForbidsWatchdogSelfRecovery(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-watchdog.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	settingsStart := strings.Index(script, "$settings = New-ScheduledTaskSettingsSet")
	settingsEnd := strings.Index(script[settingsStart:], "$task = New-ScheduledTask")
	if settingsStart < 0 || settingsEnd < 0 {
		t.Fatal("installer Scheduled Task settings block not found")
	}
	settings := script[settingsStart : settingsStart+settingsEnd]
	if strings.Contains(settings, "RestartCount") || strings.Contains(settings, "RestartInterval") {
		t.Fatalf("watchdog installer enables self recovery:\n%s", settings)
	}
	if !strings.Contains(script, "[int]$registeredTask.Settings.RestartCount -ne 0") {
		t.Fatal("installer does not verify zero Scheduled Task restart count")
	}
}

func TestModuleInstallersDefaultToWatchdogManagedTasks(t *testing.T) {
	for _, name := range []string{
		"../../scripts/install-windows-capture-agent.ps1",
		"../../scripts/install-windows-action-osd.ps1",
		"../../scripts/install-windows-event-web.ps1",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		for _, required := range []string{
			`[ValidateSet("WatchdogManaged", "Standalone")]`,
			`[string]$StartupMode = "WatchdogManaged"`,
			`if ($StartupMode -eq "Standalone")`,
			`$settingsArguments.RestartCount = 3`,
			`$taskArguments.Trigger = New-ScheduledTaskTrigger -AtLogOn`,
		} {
			if !strings.Contains(script, required) {
				t.Fatalf("%s is missing explicit startup-mode contract %q", name, required)
			}
		}
	}
}

func TestCaptureInstallerStopsExactResidentOCRRuntimeBeforeCopy(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-capture-agent.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`Assert-GUIExecutable -Path (Join-Path $sourceOCRRuntime "PpOcr.DirectML.exe")`,
		`-Label "resident OCR runtime executable"`,
		`$installedOCRExecutable = Join-Path $installedOCRRuntime "PpOcr.DirectML.exe"`,
		`Where-Object { $_.Path -eq $installedOCRExecutable }`,
		`Stop-Process -Id $residentOCRProcess.Id -Force -ErrorAction Stop`,
		`resident OCR runtime did not stop before installation`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("capture installer is missing exact resident OCR shutdown contract %q", required)
		}
	}
}
