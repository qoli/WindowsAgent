package assistinstall

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/wgcworker"
)

func TestCaptureStartupBudgetCoversRequiredInitialization(t *testing.T) {
	match := regexp.MustCompile(`(?s)id="capture-agent".*?startupGraceMs=(\d+)`).FindStringSubmatch(installerScript)
	if len(match) != 2 {
		t.Fatal("Capture Agent startup budget is missing from the setup configuration")
	}
	milliseconds, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	budget := time.Duration(milliseconds) * time.Millisecond
	minimum := wgcworker.MaxInitializationDuration + 5*time.Second // Required event-journal health check.
	if budget < minimum {
		t.Fatalf("Capture Agent startup budget %s is shorter than required initialization %s", budget, minimum)
	}
}
