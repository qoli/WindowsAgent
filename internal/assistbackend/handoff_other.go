//go:build !windows

package assistbackend

import (
	"context"
	"fmt"
	"time"
)

func startApplyHandoff(string, string, string, string, bool, int, int) error {
	return fmt.Errorf("elevated setup handoff is supported only on Windows")
}

func startUninstallHandoff(string, int, int) error {
	return fmt.Errorf("elevated uninstall handoff is supported only on Windows")
}

func startConfigureWatchdogHandoff(string, bool) error {
	return fmt.Errorf("elevated Watchdog configuration handoff is supported only on Windows")
}

func ShowHelperCompletion([]string, error) {}

func waitForProcessExit(context.Context, int, time.Duration) error {
	return fmt.Errorf("process waiting is supported only on Windows")
}
