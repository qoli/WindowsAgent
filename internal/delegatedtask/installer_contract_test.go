package delegatedtask

import (
	"os"
	"strings"
	"testing"
)

func TestInstallerOwnsPinnedStandaloneInteractiveRuntime(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-agent-pi.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`"@jmfederico/pi-web" = "1.202609.0"`,
		`"@earendil-works/pi-ai" = "0.85.1"`,
		`"@injaneity/pi-computer-use" = "0.5.1"`,
		`pinned pi-computer-use package has no Windows prebuilt helper`,
		`dedicated Pi profile must configure exactly npm:@injaneity/pi-computer-use@0.5.1`,
		`"runtime-" + $lockHash.Substring(0, 16)`,
		`windows-agent-pi executable must use PE Windows GUI subsystem 2`,
		`windows-agent-pi component owner must exist and use PE Windows GUI subsystem 2`,
		`-LogonType Interactive -RunLevel Limited`,
		`Stop-OwnedPiChildProcesses -Node $resolvedNode -Runtime $installedRuntime -Helper $helperPath`,
		`New-ScheduledTaskAction -Execute $installedComponentExecutable`,
		`New-ScheduledTaskTrigger -AtLogOn`,
		`$arguments.RestartCount = 3`,
		`$SessionDaemonTaskName`,
		`$WebTaskName`,
		`$AgentTaskName`,
		`/health`,
		`/api/machines/local/runtime?refresh=1`,
		`$value.components.sessiond.activeAgentProfile.dir -ieq $agentDir`,
		`/healthz`,
		`SessionDaemonListen, WebListen, and AgentListen must use different ports`,
		`PI WEB session daemon and Web process must both run in the signed-in interactive session`,
		`both PI WEB component owners must run in the signed-in interactive session`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("installer is missing lifecycle contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"New-NetFirewallRule",
		"Set-NetFirewallRule",
		"Remove-NetFirewallRule",
		"PI_COMPUTER_USE_ALLOW_BUILD",
		"npm install",
		"npm ci",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("installer contains forbidden behavior %q", forbidden)
		}
	}
}

func TestPiComponentOwnerHasOneExplicitRuntimeEnvironmentAndJob(t *testing.T) {
	data, err := os.ReadFile("../../cmd/windows-agent-pi-component/process_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`"PI_CODING_AGENT_DIR="+cfg.agentDir`,
		`"PI_CODING_AGENT_SESSION_DIR="+cfg.sessionDir`,
		`"PI_WEB_DATA_DIR="+cfg.piWebDataDir`,
		`"PI_WEB_CONFIG="+cfg.piWebConfig`,
		`"PI_COMPUTER_USE_WINDOWS_HELPER_PATH="+cfg.helperPath`,
		`windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`,
		`windows.AssignProcessToJobObject`,
		`windows.CREATE_NO_WINDOW`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("component launcher is missing environment contract %q", required)
		}
	}
}

func TestRuntimePreparationMigratesAuthorizationWithoutMacPackagesOrBuildFallback(t *testing.T) {
	data, err := os.ReadFile("../../scripts/prepare-windows-agent-pi-runtime.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`Copy-VerifiedFile -Source $sourceAuth`,
		`defaultProvider = [string]$settings.defaultProvider`,
		`defaultModel = [string]$settings.defaultModel`,
		`& $resolvedNpm ci`,
		`& $resolvedNpm run pi:install-computer-use`,
		`$env:Path = $nodeDirectory + [IO.Path]::PathSeparator + $env:Path`,
		`Remove-Item Env:PI_COMPUTER_USE_ALLOW_BUILD`,
		`prepared Pi profile must contain only npm:@injaneity/pi-computer-use@0.5.1`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("runtime preparation is missing migration contract %q", required)
		}
	}
	for _, forbidden := range []string{`settings.packages`, `models.json`, `PI_COMPUTER_USE_ALLOW_BUILD = "1"`} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("runtime preparation contains forbidden migration behavior %q", forbidden)
		}
	}
}

func TestMacClientUsesAuthenticatedLoopbackTunnels(t *testing.T) {
	data, err := os.ReadFile("../../scripts/windows-agent-pi-client.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`-L "${local_port}:127.0.0.1:${agent_port}"`,
		`-L "${local_web_port}:127.0.0.1:${web_port}"`,
		`delegated-api.token`,
		`Authorization: Bearer %s`,
		`/v1/delegated-tasks`,
		`/events/stream?after=`,
		`--mode steer or followUp`,
		`open "$web_url"`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("macOS client is missing host contract %q", required)
		}
	}
	for _, forbidden := range []string{"0.0.0.0", "delegated_token=\"hardcoded"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("macOS client contains forbidden behavior %q", forbidden)
		}
	}
}
