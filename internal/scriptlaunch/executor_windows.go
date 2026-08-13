//go:build windows && amd64

package scriptlaunch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
	"golang.org/x/sys/windows"
)

const maxLauncherOutputBytes = 1 << 20
const maxWGCObservationAttempts = 5
const wgcObservationRetryDelay = 100 * time.Millisecond

type LocalExecutor struct {
	launcher string
	rulesDir string
	logger   *slog.Logger
}

func NewLocalExecutor(installRoot, rulesDir string, logger *slog.Logger) (*LocalExecutor, error) {
	if installRoot == "" || !filepath.IsAbs(installRoot) {
		return nil, errors.New("script runtime install root must be absolute")
	}
	if rulesDir == "" || !filepath.IsAbs(rulesDir) {
		return nil, errors.New("rules directory must be absolute")
	}
	if logger == nil {
		return nil, errors.New("script runtime logger is required")
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
	return &LocalExecutor{launcher: launcher, rulesDir: rulesDir, logger: logger}, nil
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
	if invocation.Inputs == nil {
		return nil, errors.New("inputs object is required")
	}
	ruleStore, err := rules.New(e.rulesDir)
	if err != nil {
		return nil, fmt.Errorf("initialize Rule store: %w", err)
	}
	script, err := ruleStore.ResolveScript(invocation.Capability)
	if err != nil {
		return nil, fmt.Errorf("resolve capability %q: %w", invocation.Capability, err)
	}
	foregroundInfo, err := foreground.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground process before Script launch: %w", err)
	}
	if !strings.EqualFold(foregroundInfo.ExecutableName, script.RuleID) {
		return nil, fmt.Errorf(
			"foreground executable is %q, expected owning Rule %q",
			foregroundInfo.ExecutableName,
			script.RuleID,
		)
	}
	requestRoot, err := os.MkdirTemp("", "windowsagent-script-request-")
	if err != nil {
		return nil, fmt.Errorf("create Script request directory: %w", err)
	}
	defer os.RemoveAll(requestRoot)
	requestPath := filepath.Join(requestRoot, "request.json")
	requestBytes, err := json.Marshal(Request{Inputs: invocation.Inputs})
	if err != nil {
		return nil, fmt.Errorf("encode Script request: %w", err)
	}
	if len(requestBytes) > MaxRequestBytes {
		return nil, fmt.Errorf("Script request exceeds %d bytes", MaxRequestBytes)
	}
	if err := os.WriteFile(requestPath, requestBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write Script request: %w", err)
	}

	return runWithSilentWGCObservationRetry(
		ctx,
		invocation.Capability,
		maxWGCObservationAttempts,
		wgcObservationRetryDelay,
		e.logger,
		func() (json.RawMessage, error) {
			return e.runAttempt(ctx, invocation, requestPath, foregroundInfo)
		},
	)
}

func (e *LocalExecutor) runAttempt(
	ctx context.Context,
	invocation Invocation,
	requestPath string,
	foregroundInfo foreground.Info,
) (json.RawMessage, error) {
	var stdout limitedBuffer
	var stderr limitedBuffer
	command := exec.CommandContext(
		ctx,
		e.launcher,
		"--capability", invocation.Capability,
		"--rules-dir", e.rulesDir,
		"--request-file", requestPath,
		"--process-id", strconv.FormatUint(uint64(foregroundInfo.ProcessID), 10),
		"--process-path", foregroundInfo.ExecutablePath,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
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
