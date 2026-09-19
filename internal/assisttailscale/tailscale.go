// Package assisttailscale owns AssistGUI's installed TailscaleAdapter lifecycle.
package assisttailscale

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/assistgui"
)

var ErrUnsupported = errors.New("TailscaleAdapter lifecycle is supported only on Windows")

func Start(ctx context.Context, dataDir string, authKey []byte) (assistgui.TailscaleSnapshot, error) {
	if err := validateDataDir(dataDir); err != nil {
		zero(authKey)
		return assistgui.TailscaleSnapshot{}, err
	}
	return start(ctx, dataDir, authKey)
}

func Stop(ctx context.Context, dataDir string) (assistgui.TailscaleSnapshot, error) {
	if err := validateDataDir(dataDir); err != nil {
		return assistgui.TailscaleSnapshot{}, err
	}
	return stop(ctx, dataDir)
}

// Inspect reconciles the adapter's durable status with its current process
// identity. A status file is evidence of the last transition, not proof that
// the process which wrote it is still alive.
func Inspect(ctx context.Context, dataDir string) assistgui.TailscaleSnapshot {
	if err := validateDataDir(dataDir); err != nil {
		return failedStatus(err)
	}
	return inspect(ctx, dataDir)
}

type adapterProcess struct {
	PID       int
	SessionID uint32
}

func reconcileStatus(status assistgui.TailscaleSnapshot, processes []adapterProcess, inspectErr error) assistgui.TailscaleSnapshot {
	if inspectErr != nil {
		return failedStatus(fmt.Errorf("inspect TailscaleAdapter process: %w", inspectErr))
	}
	if status.State == assistgui.TailscaleFailed {
		return status
	}
	if status.State == assistgui.TailscaleDisabled {
		if len(processes) == 0 {
			return status
		}
		return failedStatus(errors.New("TailscaleAdapter is running while durable status is disabled"))
	}
	if len(processes) != 1 {
		return failedStatus(fmt.Errorf("TailscaleAdapter status is %s but %d installed processes are running", status.State, len(processes)))
	}
	if processes[0].PID != status.ProcessID || processes[0].SessionID == 0 {
		return failedStatus(errors.New("TailscaleAdapter status does not identify the installed interactive-session process"))
	}
	return status
}

func failedStatus(err error) assistgui.TailscaleSnapshot {
	return assistgui.TailscaleSnapshot{
		SchemaVersion: 1,
		Enabled:       true,
		State:         assistgui.TailscaleFailed,
		Error:         err.Error(),
	}
}

func validateDataDir(dataDir string) error {
	if dataDir == "" || !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return errors.New("TailscaleAdapter data directory must be a clean absolute path")
	}
	return nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
