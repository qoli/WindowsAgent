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
		"../../scripts/install-windows-sftp.ps1",
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

func TestSFTPInstallerCreatesElevatedCredentiallessFilesystemService(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-sftp.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`[string]$Listen = "0.0.0.0:2022"`,
		`[string]$StatusListen = "127.0.0.1:8793"`,
		`"host-key", "init", "--path"`,
		`if (-not (Test-Path -LiteralPath $hostKeyFile))`,
		`-LogonType Interactive -RunLevel Highest`,
		`SFTP installation must run from an elevated Administrator session`,
		`Invoke-RestMethod -Method Get -Uri $healthURI`,
		`$health.runtime -ceq "windows-sftp-v1"`,
		`$health.username -ceq "windowsagent"`,
		`$health.clientAuthentication -ceq "none"`,
		`$health.hostKeyFingerprint`,
		`type = "process"`,
		`requireInteractiveSession = $false`,
		`type = "http-json"`,
		`expectedJsonStatus = "ok"`,
		`maxAttempts = 3`,
		`attemptWindowMs = 300000`,
		`backoffMs = 5000`,
		`actionTimeoutMs = 10000`,
		`host_key_source = $hostKeySource`,
		`watchdog_target = $watchdogTarget`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("SFTP installer is missing lifecycle contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"New-NetFirewallRule",
		"Set-NetFirewallRule",
		"Remove-NetFirewallRule",
		"password",
	} {
		if strings.Contains(strings.ToLower(script), strings.ToLower(forbidden)) {
			t.Fatalf("SFTP installer contains forbidden behavior %q", forbidden)
		}
	}
}

func TestSFTPInstallerWatchdogTargetMatchesStrictConfigContract(t *testing.T) {
	config := `{
  "schemaVersion":1,
  "checkIntervalMs":1000,
  "targets":[{
    "id":"sftp",
    "desiredState":"running",
    "startAfterHealthy":[],
    "failureThreshold":3,
    "probes":[
      {"type":"process","executablePath":"C:\\\\Agent\\\\bin\\\\windows-sftp.exe","requireInteractiveSession":false},
      {"type":"http-json","url":"http://127.0.0.1:8793/healthz","expectedStatusCode":200,"expectedJsonStatus":"ok","timeoutMs":2000}
    ],
    "recovery":{
      "scheduledTaskName":"gameGuide Windows SFTP",
      "expectedTaskDescription":"gameGuide WindowsAgent SFTP filesystem data plane; elevated current-user token",
      "maxAttempts":3,
      "attemptWindowMs":300000,
      "backoffMs":5000,
      "actionTimeoutMs":10000,
      "startupGraceMs":5000
    }
  }]
}`
	parsed, err := ParseConfig([]byte(config))
	if err != nil {
		t.Fatalf("installer SFTP target is not a valid Watchdog target: %v", err)
	}
	if len(parsed.Targets) != 1 || parsed.Targets[0].ID != "sftp" ||
		len(parsed.Targets[0].Probes) != 2 {
		t.Fatalf("parsed SFTP target = %+v", parsed.Targets)
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

func TestCaptureInstallerMakesAgentRunLevelAnInstallChoice(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-capture-agent.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`[ValidateSet("Limited", "Highest")]`,
		`[string]$AgentRunLevel = "Limited"`,
		`-LogonType Interactive -RunLevel $AgentRunLevel`,
		`$eventPrincipal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited`,
		`agent_run_level = $AgentRunLevel`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("capture installer is missing Agent run-level contract %q", required)
		}
	}
}
