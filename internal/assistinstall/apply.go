// Package assistinstall applies one verified base WindowsAgent release set.
package assistinstall

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

//go:embed install.ps1
var installerScript string

type Request struct {
	StageDir    string
	CatalogPath string
	DataDir     string
}

func Apply(ctx context.Context, request Request) error {
	if request.StageDir == "" || !filepath.IsAbs(request.StageDir) {
		return errors.New("release stage directory must be absolute")
	}
	if request.CatalogPath == "" || !filepath.IsAbs(request.CatalogPath) {
		return errors.New("release catalog path must be absolute")
	}
	if request.DataDir == "" || !filepath.IsAbs(request.DataDir) {
		return errors.New("install data directory must be absolute")
	}
	expectedCatalogPath := filepath.Join(filepath.Clean(request.StageDir), "windowsagent-release.json")
	if filepath.Clean(request.CatalogPath) != expectedCatalogPath {
		return errors.New("release catalog must be windowsagent-release.json inside the staging directory")
	}
	file, err := os.Open(request.CatalogPath)
	if err != nil {
		return fmt.Errorf("open staged release catalog: %w", err)
	}
	catalog, err := releasecatalog.Load(file)
	file.Close()
	if err != nil {
		return err
	}
	if err := releasecatalog.VerifySelected(request.StageDir, catalog, releasecatalog.BaseInstallArtifact); err != nil {
		return fmt.Errorf("verify staged base release: %w", err)
	}
	sums, err := os.Open(filepath.Join(request.StageDir, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("open staged SHA256SUMS: %w", err)
	}
	sumsErr := releasecatalog.ValidateSHA256Sums(sums, catalog)
	closeErr := sums.Close()
	if sumsErr != nil {
		return fmt.Errorf("verify staged SHA256SUMS: %w", sumsErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged SHA256SUMS: %w", closeErr)
	}
	return runInstaller(ctx, request, installerScript)
}
