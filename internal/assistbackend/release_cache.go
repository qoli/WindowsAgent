package assistbackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

func stageVerifiedCachedRelease(ctx context.Context, dataDir, destination string, catalog releasecatalog.Catalog) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return false, err
	}
	temporary, err := os.MkdirTemp(filepath.Dir(destination), ".release-cache-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(temporary)
	reused, err := reuseVerifiedRelease(ctx, dataDir, temporary, catalog)
	if err != nil || !reused {
		return false, err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return false, fmt.Errorf("commit verified cached release: %w", err)
	}
	return true, nil
}

// reuseVerifiedRelease copies only runtime artifacts from a completed stage
// matching the freshly fetched catalog. The caller publishes new metadata only
// after this returns successfully, so partial copies never become cache entries.
func reuseVerifiedRelease(ctx context.Context, dataDir, destination string, catalog releasecatalog.Catalog) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := catalog.Validate(); err != nil {
		return false, err
	}
	root := filepath.Join(dataDir, "release-staging")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read release cache: %w", err)
	}
	type candidate struct {
		path     string
		modified time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		path := filepath.Join(root, entry.Name())
		if !entry.IsDir() || filepath.Clean(path) == filepath.Clean(destination) {
			continue
		}
		info, err := os.Stat(filepath.Join(path, "windowsagent-release.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("inspect completed release cache: %w", err)
		}
		candidates = append(candidates, candidate{path, info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modified.Equal(candidates[j].modified) {
			return candidates[i].path > candidates[j].path
		}
		return candidates[i].modified.After(candidates[j].modified)
	})
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		file, err := os.Open(filepath.Join(candidate.path, "windowsagent-release.json"))
		if err != nil {
			return false, fmt.Errorf("open completed release cache catalog: %w", err)
		}
		cached, loadErr := releasecatalog.LoadInstalled(file)
		closeErr := file.Close()
		if err := errors.Join(loadErr, closeErr); err != nil {
			return false, fmt.Errorf("read completed release cache catalog: %w", err)
		}
		if !reflect.DeepEqual(cached, catalog) {
			continue
		}
		if err := releasecatalog.VerifySelected(candidate.path, catalog, releasecatalog.InstallArtifact); err != nil {
			return false, fmt.Errorf("verify matching release cache: %w", err)
		}
		sums, err := os.Open(filepath.Join(candidate.path, "SHA256SUMS"))
		if err != nil {
			return false, fmt.Errorf("open matching release cache checksums: %w", err)
		}
		verifyErr := releasecatalog.ValidateSHA256Sums(sums, catalog)
		closeErr = sums.Close()
		if err := errors.Join(verifyErr, closeErr); err != nil {
			return false, fmt.Errorf("verify matching release cache checksums: %w", err)
		}
		for _, artifact := range catalog.Artifacts {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !releasecatalog.InstallArtifact(artifact) {
				continue
			}
			if err := copyCachedArtifact(filepath.Join(candidate.path, artifact.Name), filepath.Join(destination, artifact.Name)); err != nil {
				return false, fmt.Errorf("copy cached runtime artifact %s: %w", artifact.Name, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := releasecatalog.VerifySelected(destination, catalog, releasecatalog.InstallArtifact); err != nil {
			return false, fmt.Errorf("verify copied release cache: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func copyCachedArtifact(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
