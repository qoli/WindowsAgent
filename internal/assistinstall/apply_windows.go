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
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", "-")
	command.Stdin = strings.NewReader(script)
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
