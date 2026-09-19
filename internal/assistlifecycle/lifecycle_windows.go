//go:build windows

package assistlifecycle

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

//go:embed lifecycle.ps1
var lifecycleScript string

func run(ctx context.Context, action, dataDir string) (Facts, error) {
	script, err := os.CreateTemp("", "windowsagent-lifecycle-*.ps1")
	if err != nil {
		return Facts{}, fmt.Errorf("create WindowsAgent lifecycle script: %w", err)
	}
	name := script.Name()
	defer os.Remove(name)
	if _, err := script.WriteString(lifecycleScript); err != nil {
		script.Close()
		return Facts{}, fmt.Errorf("write WindowsAgent lifecycle script: %w", err)
	}
	if err := script.Close(); err != nil {
		return Facts{}, fmt.Errorf("close WindowsAgent lifecycle script: %w", err)
	}
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", name)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	command.Env = append(os.Environ(), "WINDOWSAGENT_LIFECYCLE_ACTION="+action, "WINDOWSAGENT_INSTALL_DATA_DIR="+dataDir)
	output, err := command.CombinedOutput()
	if err != nil {
		return Facts{}, fmt.Errorf("%s WindowsAgent: %w: %s", action, err, strings.TrimSpace(string(output)))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var facts Facts
	if err := decoder.Decode(&facts); err != nil {
		return Facts{}, fmt.Errorf("decode %s lifecycle result: %w", action, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Facts{}, errors.New("lifecycle script returned multiple JSON values")
		}
		return Facts{}, fmt.Errorf("decode lifecycle result suffix: %w", err)
	}
	if facts.Processes == nil {
		facts.Processes = []ProcessFact{}
	}
	return facts, nil
}
