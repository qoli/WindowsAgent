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
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/observationjob"
	"github.com/qoli/WindowsAgent/internal/observer"
)

const crimsonInventoryCapability = "crimson-desert/inventory"

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
	var capability, installRoot, saveRoot, saveRelative, processPath string
	var processID uint
	flags.StringVar(&capability, "capability", "", "registered observation capability ID")
	flags.StringVar(&installRoot, "install-root", "", "absolute WindowsAgent observation runtime root")
	flags.StringVar(&saveRoot, "save-root", "", "authorized absolute save root")
	flags.StringVar(&saveRelative, "save-relative", "", "selected root-relative save file")
	flags.UintVar(&processID, "process-id", 0, "host-resolved process ID; zero uses foreground")
	flags.StringVar(&processPath, "process-path", "", "absolute image path required with process-id")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if capability != crimsonInventoryCapability {
		return fmt.Errorf("unsupported registered capability %q", capability)
	}
	if installRoot == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		installRoot = filepath.Dir(executable)
	}
	if !filepath.IsAbs(installRoot) || !filepath.IsAbs(saveRoot) || saveRelative == "" {
		return errors.New("install-root, save-root, and save-relative must identify explicit paths")
	}

	var resolvedProcessID uint32
	var resolvedProcessPath string
	if processID == 0 {
		foregroundInfo, err := foreground.Snapshot()
		if err != nil {
			return fmt.Errorf("resolve foreground process: %w", err)
		}
		if foregroundInfo.ExecutableName != "CrimsonDesert.exe" {
			return fmt.Errorf("foreground executable is %q, expected CrimsonDesert.exe", foregroundInfo.ExecutableName)
		}
		resolvedProcessID = foregroundInfo.ProcessID
		resolvedProcessPath = foregroundInfo.ExecutablePath
	} else {
		if processID > uint(^uint32(0)) || !filepath.IsAbs(processPath) ||
			filepath.Base(processPath) != "CrimsonDesert.exe" {
			return errors.New("process-id requires an absolute CrimsonDesert.exe process-path")
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
	deadline := time.Now().Add(20 * time.Second)
	result, err := observationjob.Run(context.Background(), observationjob.Spec{
		JobID:                  jobID,
		Deadline:               deadline,
		PackageRoot:            filepath.Join(installRoot, "ObservationScripts", "CrimsonDesert", "inventory"),
		ScriptRunnerExecutable: filepath.Join(installRoot, "windows-observation-script-runner.exe"),
		ObserverExecutable:     filepath.Join(installRoot, "windows-observer.exe"),
		Process:                &process,
		FileRoots: map[string]string{
			"crimson-desert-saves": saveRoot,
		},
		Inputs: map[string]any{
			"save": map[string]any{
				"root":     "crimson-desert-saves",
				"relative": saveRelative,
			},
		},
	})
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string]any{
		"ok":         true,
		"jobId":      jobID,
		"capability": capability,
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
