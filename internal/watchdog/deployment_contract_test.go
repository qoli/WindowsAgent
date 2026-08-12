package watchdog

import (
	"os"
	"strings"
	"testing"
)

func TestBinaryDeploymentUsesExactInstalledWatchdogTargets(t *testing.T) {
	data, err := os.ReadFile("../../scripts/deploy-windows-binaries.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`$targets = @($config.targets)`,
		`foreach ($target in $targets)`,
		`foreach ($probe in @($target.probes))`,
		`Invoke-WebRequest -Uri ([string]$probe.url)`,
		`target Scheduled Task ownership mismatch`,
		`installed Watchdog tasks do not map the complete binary set`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("deployment coordinator is missing Watchdog target contract %q", required)
		}
	}
}
