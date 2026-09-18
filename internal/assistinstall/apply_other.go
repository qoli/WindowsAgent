//go:build !windows

package assistinstall

import (
	"context"
	"errors"
)

func runInstaller(context.Context, Request, string) error {
	return errors.New("WindowsAgent release installation requires Windows")
}

func runUninstaller(context.Context, string, string) error {
	return errors.New("WindowsAgent uninstallation requires Windows")
}

func runWatchdogConfigurator(context.Context, string, bool, string) error {
	return errors.New("WindowsAgent Watchdog configuration requires Windows")
}
