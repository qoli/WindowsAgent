package watchdog

import (
	"os"
	"strings"
	"testing"
)

func TestObservationProcessInstallerCreatesWatchdogOwnedResidentTasks(t *testing.T) {
	data, err := os.ReadFile("../../scripts/install-windows-observation-processes.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`$EvidenceTaskName = "gameGuide Windows Evidence Recorder"`,
		`$VisualLogTaskName = "gameGuide Windows Visual Log"`,
		`$evidenceDescription = "gameGuide independent resident Evidence control service; interactive-user session required"`,
		`$visualLogDescription = "gameGuide independent resident Visual Log control service; interactive-user session required"`,
		`-PreviousDescriptions @("gameGuide independent finite Evidence recorder; interactive-user session required")`,
		`-PreviousDescriptions @("gameGuide independent on-demand Visual Log; interactive-user session required")`,
		`VisualLogModelBaseURL is required for the first installation`,
		`-AllowVisualLogModelBaseURLChange to change it explicitly`,
		`Assert-VisualLogModelEndpoint -BaseURL $resolvedModelBaseURL`,
		`model_base_url_source = $modelBaseURLResolution.Source`,
		`watchdog_managed = $true`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("observation process installer is missing independent task contract %q", required)
		}
	}
	if strings.Contains(script, "New-ScheduledTaskTrigger") || strings.Contains(script, "RestartCount =") ||
		strings.Contains(script, "RestartInterval =") {
		t.Fatal("observation process installer must leave startup and recovery exclusively to the watchdog")
	}
}
