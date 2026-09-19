//go:build windows

package assisttailscale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/assistgui"
	"github.com/qoli/WindowsAgent/internal/releasecatalog"
	"github.com/qoli/WindowsAgent/internal/watchdog"
)

func start(ctx context.Context, dataDir string, authKey []byte) (assistgui.TailscaleSnapshot, error) {
	defer zero(authKey)
	if len(authKey) == 0 {
		return assistgui.TailscaleSnapshot{}, errors.New("a Tailscale auth key is required")
	}
	binDir := filepath.Join(dataDir, "bin")
	adapterPath := filepath.Join(binDir, "windows-tailscale-adapter.exe")
	if err := verifyAdapterArtifact(binDir); err != nil {
		return assistgui.TailscaleSnapshot{}, err
	}
	processes, err := (watchdog.WindowsProcessInspector{}).FindByExecutablePath(ctx, adapterPath)
	if err != nil {
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("inspect TailscaleAdapter process: %w", err)
	}
	if len(processes) != 0 {
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("TailscaleAdapter is already running from the installed path")
	}
	adapterDir := filepath.Join(dataDir, "tailscale")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("create TailscaleAdapter data directory: %w", err)
	}
	stopFile := filepath.Join(adapterDir, "stop.request")
	if err := os.Remove(stopFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("clear stale TailscaleAdapter stop request: %w", err)
	}
	command := exec.Command(adapterPath,
		"--data-dir", adapterDir,
		"--status-file", filepath.Join(adapterDir, "status.json"),
		"--stop-file", stopFile,
		"--listen", ":8787",
		"--forward-to", "127.0.0.1:8787",
	)
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("create TailscaleAdapter secret pipe: %w", err)
	}
	command.Stdin = readPipe
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		readPipe.Close()
		writePipe.Close()
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("start TailscaleAdapter: %w", err)
	}
	_ = readPipe.Close()
	payload := make([]byte, len(authKey)+1)
	copy(payload, authKey)
	payload[len(payload)-1] = '\n'
	_, writeErr := writePipe.Write(payload)
	zero(payload)
	closeErr := writePipe.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.WriteFile(stopFile, []byte("stop\n"), 0o600)
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("send auth key to TailscaleAdapter: %v %v", writeErr, closeErr)
	}
	if err := command.Process.Release(); err != nil {
		_ = os.WriteFile(stopFile, []byte("stop\n"), 0o600)
		return assistgui.TailscaleSnapshot{}, fmt.Errorf("release TailscaleAdapter process handle: %w", err)
	}
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return assistgui.TailscaleSnapshot{}, ctx.Err()
		case <-deadline.C:
			return assistgui.TailscaleSnapshot{}, errors.New("TailscaleAdapter did not become ONLINE within 45 seconds")
		case <-ticker.C:
			status := assistgui.LoadTailscaleStatus(dataDir)
			if status.State == assistgui.TailscaleFailed {
				return status, fmt.Errorf("TailscaleAdapter failed: %s", status.Error)
			}
			if status.State == assistgui.TailscaleOnline {
				if err := verifyAdapterProcess(ctx, adapterPath, status); err != nil {
					return status, err
				}
				return status, nil
			}
		}
	}
}

func stop(ctx context.Context, dataDir string) (assistgui.TailscaleSnapshot, error) {
	adapterPath := filepath.Join(dataDir, "bin", "windows-tailscale-adapter.exe")
	status := assistgui.LoadTailscaleStatus(dataDir)
	processes, err := (watchdog.WindowsProcessInspector{}).FindByExecutablePath(ctx, adapterPath)
	if err != nil {
		return status, fmt.Errorf("inspect TailscaleAdapter process: %w", err)
	}
	if status.State == assistgui.TailscaleDisabled {
		if len(processes) != 0 {
			return status, errors.New("TailscaleAdapter is running without enabled status")
		}
		return status, nil
	}
	if !status.Enabled || len(processes) != 1 {
		return status, errors.New("TailscaleAdapter status and installed process identity disagree")
	}
	if err := verifyAdapterProcess(ctx, adapterPath, status); err != nil {
		return status, err
	}
	stopFile := filepath.Join(dataDir, "tailscale", "stop.request")
	if err := os.WriteFile(stopFile, []byte("stop\n"), 0o600); err != nil {
		return status, fmt.Errorf("request TailscaleAdapter stop: %w", err)
	}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-deadline.C:
			return status, errors.New("TailscaleAdapter did not confirm logout and DISABLED state within 20 seconds")
		case <-ticker.C:
			status = assistgui.LoadTailscaleStatus(dataDir)
			processes, err = (watchdog.WindowsProcessInspector{}).FindByExecutablePath(ctx, adapterPath)
			if err != nil {
				return status, err
			}
			if status.State == assistgui.TailscaleDisabled && len(processes) == 0 {
				return status, nil
			}
		}
	}
}

func inspect(ctx context.Context, dataDir string) assistgui.TailscaleSnapshot {
	status := assistgui.LoadTailscaleStatus(dataDir)
	adapterPath := filepath.Join(dataDir, "bin", "windows-tailscale-adapter.exe")
	processes, err := (watchdog.WindowsProcessInspector{}).FindByExecutablePath(ctx, adapterPath)
	identities := make([]adapterProcess, 0, len(processes))
	for _, process := range processes {
		identities = append(identities, adapterProcess{PID: int(process.PID), SessionID: process.SessionID})
	}
	return reconcileStatus(status, identities, err)
}

func verifyAdapterArtifact(binDir string) error {
	file, err := os.Open(filepath.Join(binDir, "windowsagent-release.json"))
	if err != nil {
		return fmt.Errorf("open installed release catalog: %w", err)
	}
	catalog, loadErr := releasecatalog.LoadInstalled(file)
	closeErr := file.Close()
	if loadErr != nil {
		return loadErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := releasecatalog.VerifyInstalledArtifact(binDir, catalog, "windows-tailscale-adapter.exe"); err != nil {
		return fmt.Errorf("verify installed TailscaleAdapter: %w", err)
	}
	return nil
}

func verifyAdapterProcess(ctx context.Context, path string, status assistgui.TailscaleSnapshot) error {
	if status.ProcessID <= 0 {
		return errors.New("TailscaleAdapter status process ID is missing")
	}
	processes, err := (watchdog.WindowsProcessInspector{}).FindByExecutablePath(ctx, path)
	if err != nil {
		return err
	}
	if len(processes) != 1 || int(processes[0].PID) != status.ProcessID || processes[0].SessionID == 0 {
		return errors.New("TailscaleAdapter status does not identify the installed interactive-session process")
	}
	return nil
}
