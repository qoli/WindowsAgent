//go:build windows && amd64

package scriptlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxLauncherOutputBytes = 1 << 20

type LocalExecutor struct {
	launcher string
	rulesDir string
}

func NewLocalExecutor(installRoot, rulesDir string) (*LocalExecutor, error) {
	if installRoot == "" || !filepath.IsAbs(installRoot) {
		return nil, errors.New("script runtime install root must be absolute")
	}
	if rulesDir == "" || !filepath.IsAbs(rulesDir) {
		return nil, errors.New("rules directory must be absolute")
	}
	launcher := filepath.Join(installRoot, "windows-observation-job.exe")
	for name, target := range map[string]string{
		"script launcher": launcher,
		"Script Runner":   filepath.Join(installRoot, "windows-observation-script-runner.exe"),
		"Observer":        filepath.Join(installRoot, "windows-observer.exe"),
	} {
		info, err := os.Stat(target)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s must be a regular file", name)
		}
	}
	return &LocalExecutor{launcher: launcher, rulesDir: rulesDir}, nil
}

func (e *LocalExecutor) Run(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	if e == nil {
		return nil, errors.New("local Script executor is required")
	}
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if invocation.Capability == "" ||
		strings.TrimSpace(invocation.Capability) != invocation.Capability {
		return nil, errors.New("capability is required and must be canonical")
	}
	if invocation.Inputs == nil || invocation.FileRoots == nil {
		return nil, errors.New("inputs and fileRoots objects are required")
	}
	requestRoot, err := os.MkdirTemp("", "windowsagent-script-request-")
	if err != nil {
		return nil, fmt.Errorf("create Script request directory: %w", err)
	}
	defer os.RemoveAll(requestRoot)
	requestPath := filepath.Join(requestRoot, "request.json")
	requestBytes, err := json.Marshal(Request{
		Inputs:    invocation.Inputs,
		FileRoots: invocation.FileRoots,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Script request: %w", err)
	}
	if len(requestBytes) > MaxRequestBytes {
		return nil, fmt.Errorf("Script request exceeds %d bytes", MaxRequestBytes)
	}
	if err := os.WriteFile(requestPath, requestBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write Script request: %w", err)
	}

	var stdout limitedBuffer
	var stderr limitedBuffer
	command := exec.CommandContext(
		ctx,
		e.launcher,
		"--capability", invocation.Capability,
		"--rules-dir", e.rulesDir,
		"--request-file", requestPath,
	)
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("Script launcher output exceeded its bound")
	}
	return parseLauncherOutput(bytes.TrimSpace(stdout.Bytes()), runErr)
}

type limitedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxLauncherOutputBytes {
		b.exceeded = true
		return 0, errors.New("buffer limit exceeded")
	}
	return b.Buffer.Write(data)
}
