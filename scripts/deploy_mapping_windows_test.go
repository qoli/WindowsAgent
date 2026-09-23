//go:build windows

package installerscripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeploymentMappingSupportsAssistAndConfiguredCompanions(t *testing.T) {
	start := strings.Index(DeployWindowsBinaries, "$expectedNames = @(")
	end := strings.Index(DeployWindowsBinaries, "function Get-Sha256 {")
	if start < 0 || end <= start {
		t.Fatal("installed mapping function is missing")
	}
	script := "Set-StrictMode -Version Latest\n$ErrorActionPreference = 'Stop'\n" + DeployWindowsBinaries[start:end] + `
function New-BaseMapping {
    @{
        "windows-capture-agent.exe" = "C:\Agent\bin\windows-capture-agent.exe"
        "windows-event-stream.exe" = "C:\Agent\events\windows-event-stream.exe"
        "windows-watchdog.exe" = "C:\Agent\watchdog\windows-watchdog.exe"
    }
}
function Assert-Rejected([hashtable]$Mapping, [string]$Message) {
    $caught = $false
    try { Complete-InstalledBinaryMapping -Destinations $Mapping } catch {
        if (-not $_.Exception.Message.Contains($Message)) { throw }
        $caught = $true
    }
    if (-not $caught) { throw "expected mapping rejection: $Message" }
}
$minimal = New-BaseMapping
Complete-InstalledBinaryMapping -Destinations $minimal
if ($minimal.Count -ne 12) { throw "Assist default mapping omitted installed artifacts" }
if ($minimal["windows-sftp.exe"] -cne "C:\Agent\bin\windows-sftp.exe") { throw "unconfigured companion must use Capture bin directory" }
if ($minimal["windows-event-stream.exe"] -cne "C:\Agent\events\windows-event-stream.exe") { throw "event task path was changed" }
foreach ($name in $expectedNames) {
    if (-not $minimal.ContainsKey($name)) { throw "missing payload destination: $name" }
}
$configured = New-BaseMapping
foreach ($name in @("windows-action-osd.exe", "windows-evidence-recorder.exe", "windows-visual-log.exe", "windows-event-web.exe", "windows-sftp.exe")) {
    $configured[$name] = "C:\Agent\custom\$name"
}
$before = $configured.Clone()
Complete-InstalledBinaryMapping -Destinations $configured
if ($configured.Count -ne 12) { throw "complete configured mapping omitted installed artifacts" }
foreach ($name in $before.Keys) {
    if ($configured[$name] -cne $before[$name]) { throw "configured task path changed: $name" }
}
foreach ($name in @("windows-capture-agent.exe", "windows-event-stream.exe", "windows-watchdog.exe")) {
    $missing = New-BaseMapping
    $missing.Remove($name)
    Assert-Rejected $missing "required binary: $name"
}
$unknown = New-BaseMapping
$unknown["unexpected.exe"] = "C:\Agent\bin\unexpected.exe"
Assert-Rejected $unknown "do not map the complete binary set"
$internalTarget = New-BaseMapping
$internalTarget["windows-wgc-worker.exe"] = "C:\Agent\bin\windows-wgc-worker.exe"
Assert-Rejected $internalTarget "unexpectedly owns internal binary"
`
	path := filepath.Join(t.TempDir(), "mapping-test.ps1")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path).CombinedOutput()
	if err != nil {
		t.Fatalf("native mapping test: %v\n%s", err, output)
	}
}
