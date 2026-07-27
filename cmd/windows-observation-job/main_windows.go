//go:build windows && amd64

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/observationjob"
	"github.com/qoli/WindowsAgent/internal/observer"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
)

const launcherDeadline = 75 * time.Second

func main() {
	if err := run(); err != nil {
		encoded, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		fmt.Fprintln(os.Stdout, string(encoded))
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("windows-observation-job", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var capability, installRoot, rulesDir, requestFile, processPath string
	var processID uint
	flags.StringVar(&capability, "capability", "", "registered observation capability ID")
	flags.StringVar(&installRoot, "install-root", "", "absolute WindowsAgent observation runtime root")
	flags.StringVar(&rulesDir, "rules-dir", "", "absolute external Rule plugin directory")
	flags.StringVar(&requestFile, "request-file", "", "absolute strict-JSON launcher request file")
	flags.UintVar(&processID, "process-id", 0, "host-resolved owning Rule process ID; zero uses foreground")
	flags.StringVar(&processPath, "process-path", "", "absolute owning Rule image path required with process-id")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if installRoot == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		installRoot = filepath.Dir(executable)
	}
	if !filepath.IsAbs(installRoot) || !filepath.IsAbs(rulesDir) ||
		!filepath.IsAbs(requestFile) {
		return errors.New("install-root, rules-dir, and request-file must be absolute paths")
	}
	request, err := scriptlaunch.ReadRequest(requestFile)
	if err != nil {
		return err
	}
	ruleStore, err := rules.New(rulesDir)
	if err != nil {
		return fmt.Errorf("initialize rule store: %w", err)
	}
	script, err := ruleStore.ResolveScript(capability)
	if err != nil {
		return fmt.Errorf("resolve capability %q: %w", capability, err)
	}
	if script.Runtime != rules.ObservationRuntimeV1 {
		return fmt.Errorf("unsupported script runtime %q for capability %q", script.Runtime, capability)
	}
	var resolvedProcessID uint32
	var resolvedProcessPath string
	if processID == 0 {
		if processPath != "" {
			return errors.New("process-path requires process-id")
		}
		foregroundInfo, err := foreground.Snapshot()
		if err != nil {
			return fmt.Errorf("resolve foreground process: %w", err)
		}
		if !strings.EqualFold(foregroundInfo.ExecutableName, script.RuleID) {
			return fmt.Errorf(
				"foreground executable is %q, expected owning Rule %q",
				foregroundInfo.ExecutableName,
				script.RuleID,
			)
		}
		resolvedProcessID = foregroundInfo.ProcessID
		resolvedProcessPath = foregroundInfo.ExecutablePath
	} else {
		if processID > uint(^uint32(0)) || !filepath.IsAbs(processPath) ||
			!strings.EqualFold(filepath.Base(processPath), script.RuleID) {
			return fmt.Errorf(
				"process-id requires an absolute owning Rule %s process-path",
				script.RuleID,
			)
		}
		resolvedProcessID = uint32(processID)
		resolvedProcessPath = processPath
	}
	process, err := observer.ResolveProcessIdentity(resolvedProcessID, resolvedProcessPath)
	if err != nil {
		return err
	}
	jobID, err := newJobID()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(launcherDeadline)
	result, err := observationjob.Run(context.Background(), observationjob.Spec{
		JobID:                  jobID,
		Deadline:               deadline,
		CapabilityID:           capability,
		PackageRoot:            script.Root,
		ScriptRunnerExecutable: filepath.Join(installRoot, "windows-observation-script-runner.exe"),
		ObserverExecutable:     filepath.Join(installRoot, "windows-observer.exe"),
		Process:                &process,
		LocalAppData:           os.Getenv("LOCALAPPDATA"),
		Inputs:                 request.Inputs,
	})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string]any{
		"ok":         true,
		"jobId":      jobID,
		"capability": capability,
		"ruleId":     script.RuleID,
		"output":     result.Output,
		"provenance": result.Provenance,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(encoded))
	return nil
}

func newJobID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
