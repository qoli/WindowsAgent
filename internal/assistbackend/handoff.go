package assistbackend

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/qoli/WindowsAgent/internal/assistinstall"
)

func RunElevatedHelper(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--assist-apply":
		if len(args) != 8 {
			return true, errors.New("--assist-apply requires operation, stage, catalog, data directory, Watchdog setting, client process ID, and backend process ID")
		}
		startAtSignIn, err := strconv.ParseBool(args[5])
		if err != nil {
			return true, fmt.Errorf("invalid Watchdog setting: %w", err)
		}
		clientPID, err := positivePID(args[6])
		if err != nil {
			return true, err
		}
		backendPID, err := positivePID(args[7])
		if err != nil {
			return true, err
		}
		if err := waitForProcessExit(ctx, clientPID, 2*time.Minute); err != nil {
			return true, err
		}
		if err := waitForProcessExit(ctx, backendPID, 2*time.Minute); err != nil {
			return true, err
		}
		operation := assistinstall.Operation(args[1])
		err = assistinstall.Apply(ctx, assistinstall.Request{
			Operation: operation, StageDir: args[2], CatalogPath: args[3], DataDir: args[4], WatchdogStartAtLogon: startAtSignIn,
		})
		return true, err
	case "--assist-uninstall":
		if len(args) != 4 {
			return true, errors.New("--assist-uninstall requires data directory, client process ID, and backend process ID")
		}
		clientPID, err := positivePID(args[2])
		if err != nil {
			return true, err
		}
		backendPID, err := positivePID(args[3])
		if err != nil {
			return true, err
		}
		if err := waitForProcessExit(ctx, clientPID, 2*time.Minute); err != nil {
			return true, err
		}
		if err := waitForProcessExit(ctx, backendPID, 2*time.Minute); err != nil {
			return true, err
		}
		return true, assistinstall.Uninstall(ctx, args[1])
	case "--assist-configure-watchdog":
		if len(args) != 3 {
			return true, errors.New("--assist-configure-watchdog requires data directory and Watchdog setting")
		}
		startAtSignIn, err := strconv.ParseBool(args[2])
		if err != nil {
			return true, fmt.Errorf("invalid Watchdog setting: %w", err)
		}
		return true, assistinstall.ConfigureWatchdog(ctx, args[1], startAtSignIn)
	default:
		return true, fmt.Errorf("unexpected backend arguments: %v", args)
	}
}

func positivePID(value string) (int, error) {
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid process ID %q", value)
	}
	return pid, nil
}

func stageUninstallHelper(dataDir string) (string, error) {
	return stageCurrentHelper(dataDir, "uninstall")
}

func stageCurrentHelper(dataDir, operation string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve Assist backend executable: %w", err)
	}
	stage := filepath.Join(dataDir, "release-staging", operation+"-"+fmt.Sprint(time.Now().UTC().UnixNano()))
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return "", fmt.Errorf("create %s staging directory: %w", operation, err)
	}
	destination := filepath.Join(stage, "windows-assist-backend.exe")
	data, err := os.ReadFile(executable)
	if err != nil {
		return "", fmt.Errorf("read Assist backend for staging: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o700); err != nil {
		return "", fmt.Errorf("write staged Assist backend: %w", err)
	}
	written, err := os.ReadFile(destination)
	if err != nil {
		return "", fmt.Errorf("read staged Assist backend: %w", err)
	}
	if sha256.Sum256(written) != sha256.Sum256(data) {
		return "", errors.New("staged Assist backend SHA-256 mismatch")
	}
	return destination, nil
}
