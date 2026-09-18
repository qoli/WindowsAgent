//go:build windows

package assistinstall

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runInstaller(ctx context.Context, request Request, script string) error {
	scriptFile, err := os.CreateTemp("", "windowsagent-install-*.ps1")
	if err != nil {
		return fmt.Errorf("create WindowsAgent installer script: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)
	if _, err := scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		return fmt.Errorf("write WindowsAgent installer script: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return fmt.Errorf("close WindowsAgent installer script: %w", err)
	}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(),
		"WINDOWSAGENT_RELEASE_STAGE="+request.StageDir,
		"WINDOWSAGENT_RELEASE_CATALOG="+request.CatalogPath,
		"WINDOWSAGENT_INSTALL_DATA_DIR="+request.DataDir,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply WindowsAgent release: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
