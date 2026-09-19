// Package assistlifecycle wraps the installed WindowsAgent Watchdog lifecycle
// contract for AssistGUI.
package assistlifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrUnsupported = errors.New("WindowsAgent lifecycle is supported only on Windows")

type Facts struct {
	Installed            bool          `json:"installed"`
	Version              string        `json:"version,omitempty"`
	WatchdogInstalled    bool          `json:"watchdogInstalled"`
	WatchdogRunning      bool          `json:"watchdogRunning"`
	WatchdogStartAtLogon bool          `json:"watchdogStartAtSignIn"`
	Processes            []ProcessFact `json:"processes"`
	TailscaleState       string        `json:"tailscaleState"`
}

type ProcessFact struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	ProcessID int    `json:"processId"`
	SessionID int    `json:"sessionId"`
}

func Inspect(ctx context.Context, dataDir string) (Facts, error) {
	if err := validateDataDir(dataDir); err != nil {
		return Facts{}, err
	}
	if err := validateInstalledCatalogFile(dataDir, false); err != nil {
		return Facts{}, err
	}
	return run(ctx, "inspect", dataDir)
}

func Start(ctx context.Context, dataDir string) (Facts, error) {
	if err := validateDataDir(dataDir); err != nil {
		return Facts{}, err
	}
	if err := validateInstalledCatalogFile(dataDir, true); err != nil {
		return Facts{}, err
	}
	return run(ctx, "start", dataDir)
}

func Stop(ctx context.Context, dataDir string) (Facts, error) {
	if err := validateDataDir(dataDir); err != nil {
		return Facts{}, err
	}
	if err := validateInstalledCatalogFile(dataDir, true); err != nil {
		return Facts{}, err
	}
	return run(ctx, "stop", dataDir)
}

func validateDataDir(dataDir string) error {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return errors.New("WindowsAgent data directory must be absolute")
	}
	if filepath.Clean(dataDir) != dataDir {
		return fmt.Errorf("WindowsAgent data directory must be clean: %q", dataDir)
	}
	return nil
}

func validateInstalledCatalogFile(dataDir string, required bool) error {
	binDir := filepath.Join(dataDir, "bin")
	catalogPath := filepath.Join(binDir, "windowsagent-release.json")
	info, err := os.Stat(catalogPath)
	if errors.Is(err, os.ErrNotExist) && !required {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat installed release catalog: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return errors.New("installed release catalog must be a regular file between 1 byte and 1 MiB")
	}
	return nil
}
