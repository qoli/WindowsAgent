//go:build windows

package assistinstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	installerscripts "github.com/qoli/WindowsAgent/scripts"
)

func runInstaller(ctx context.Context, request Request, script string) error {
	scriptDir, err := os.MkdirTemp("", "windowsagent-install-*")
	if err != nil {
		return fmt.Errorf("create WindowsAgent installer directory: %w", err)
	}
	defer os.RemoveAll(scriptDir)
	scripts := map[string]string{
		"install.ps1":                       script,
		"install-windows-capture-agent.ps1": installerscripts.InstallWindowsCaptureAgent,
		"sync-windows-agent-rule.ps1":       installerscripts.SyncWindowsAgentRule,
		"install-windows-watchdog.ps1":      installerscripts.InstallWindowsWatchdog,
		"deploy-windows-binaries.ps1":       installerscripts.DeployWindowsBinaries,
	}
	for name, contents := range scripts {
		if err := os.WriteFile(filepath.Join(scriptDir, name), []byte(contents), 0o600); err != nil {
			return fmt.Errorf("write WindowsAgent installer script %s: %w", name, err)
		}
	}
	scriptPath := filepath.Join(scriptDir, "install.ps1")
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(),
		"WINDOWSAGENT_SETUP_OPERATION="+string(request.Operation),
		"WINDOWSAGENT_RELEASE_STAGE="+request.StageDir,
		"WINDOWSAGENT_RELEASE_CATALOG="+request.CatalogPath,
		"WINDOWSAGENT_INSTALL_DATA_DIR="+request.DataDir,
		fmt.Sprintf("WINDOWSAGENT_WATCHDOG_START_AT_LOGON=%t", request.WatchdogStartAtLogon),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply WindowsAgent release: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runUninstaller(ctx context.Context, dataDir, script string) error {
	scriptFile, err := os.CreateTemp("", "windowsagent-uninstall-*.ps1")
	if err != nil {
		return fmt.Errorf("create WindowsAgent uninstaller script: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)
	if _, err := scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		return fmt.Errorf("write WindowsAgent uninstaller script: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return fmt.Errorf("close WindowsAgent uninstaller script: %w", err)
	}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "WINDOWSAGENT_INSTALL_DATA_DIR="+dataDir)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uninstall WindowsAgent: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runWatchdogConfigurator(ctx context.Context, dataDir string, startAtLogon bool, script string) error {
	scriptDir, err := os.MkdirTemp("", "windowsagent-watchdog-configure-*")
	if err != nil {
		return fmt.Errorf("create Watchdog configurator directory: %w", err)
	}
	defer os.RemoveAll(scriptDir)
	for name, contents := range map[string]string{
		"configure-watchdog.ps1":       script,
		"install-windows-watchdog.ps1": installerscripts.InstallWindowsWatchdog,
	} {
		if err := os.WriteFile(filepath.Join(scriptDir, name), []byte(contents), 0o600); err != nil {
			return fmt.Errorf("write Watchdog configurator script %s: %w", name, err)
		}
	}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(scriptDir, "configure-watchdog.ps1"))
	command.Env = append(os.Environ(),
		"WINDOWSAGENT_INSTALL_DATA_DIR="+dataDir,
		fmt.Sprintf("WINDOWSAGENT_WATCHDOG_START_AT_LOGON=%t", startAtLogon),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure WindowsAgent Watchdog: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
