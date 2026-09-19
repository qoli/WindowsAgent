package assistlifecycle

import (
	"os"
	"strings"
	"testing"
)

func TestLifecycleScriptStopsWatchdogBeforeTargetsAndTailscaleLast(t *testing.T) {
	data, err := os.ReadFile("lifecycle.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	watchdog := strings.Index(script, `Stop-ScheduledTask -TaskName $watchdogTaskName`)
	targets := strings.Index(script, `foreach ($task in $tasks) { Stop-ScheduledTask`)
	tailscale := strings.LastIndex(script, `Stop-TailscaleAdapter`)
	if watchdog < 0 || targets < 0 || tailscale < 0 || !(watchdog < targets && targets < tailscale) {
		t.Fatalf("stop order watchdog=%d targets=%d tailscale=%d", watchdog, targets, tailscale)
	}
}

func TestLifecycleScriptExcludesTailscaleFromForcedRuntimeStop(t *testing.T) {
	data, err := os.ReadFile("lifecycle.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`[string]$_.role -cne "tailscale-adapter"`,
		`Wait-PathsStopped -Paths $allRuntimePaths -Seconds 30 -ForceAfterTimeout $true`,
		`Wait-PathsStopped -Paths @($tailscaleExecutable) -Seconds 20 -ForceAfterTimeout $false`,
		`[StringComparison]::OrdinalIgnoreCase`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("lifecycle script missing %q", required)
		}
	}
}

func TestLifecycleScriptKeepsInspectAndStopAvailableWhenConfigOrTasksAreMissing(t *testing.T) {
	data, err := os.ReadFile("lifecycle.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`elseif ($action -ceq "start")`,
		`if ($action -ceq "start") { throw "Watchdog target Scheduled Task is missing; run Repair: $taskName" }`,
		`"gameGuide Windows Capture Agent" = "gameGuide Go WGC screenshot agent; interactive-user session required"`,
		`"gameGuide Windows Event Stream" = "gameGuide durable local event stream; interactive-user session required"`,
		`if ($null -ne $watchdogTask) { Stop-ScheduledTask -TaskName $watchdogTaskName`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("lifecycle script missing %q", required)
		}
	}
}
