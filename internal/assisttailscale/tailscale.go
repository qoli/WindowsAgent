// Package assisttailscale owns AssistGUI's installed TailscaleAdapter lifecycle.
package assisttailscale

import (
	"context"
	"errors"
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
