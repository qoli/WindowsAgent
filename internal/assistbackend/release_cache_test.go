package assistbackend

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/releasecatalog"
)

func releaseCacheFixture(t *testing.T, dataDir, name, version string) (string, releasecatalog.Catalog) {
	t.Helper()
	stage := filepath.Join(dataDir, "release-staging", name)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, spec := range releasecatalog.Specs() {
		var data bytes.Buffer
		header := pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, SizeOfOptionalHeader: 240}
		subsystem := uint16(2)
		if spec.Subsystem == releasecatalog.SubsystemConsole {
			subsystem = 3
		}
		optional := pe.OptionalHeader64{Magic: 0x20b, Subsystem: subsystem, NumberOfRvaAndSizes: 16}
		if err := binary.Write(&data, binary.LittleEndian, header); err != nil {
			t.Fatal(err)
		}
		if err := binary.Write(&data, binary.LittleEndian, optional); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, spec.Name), data.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := releasecatalog.Generate(stage, version)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMetadata(filepath.Join(stage, "windowsagent-release.json"), filepath.Join(stage, "SHA256SUMS"), catalog); err != nil {
		t.Fatal(err)
	}
	return stage, catalog
}

func TestReuseVerifiedReleaseCopiesOnlyRuntime(t *testing.T) {
	root := t.TempDir()
	stage, catalog := releaseCacheFixture(t, root, "complete", "0.1.13")
	destination := t.TempDir()
	reused, err := reuseVerifiedRelease(context.Background(), root, destination, catalog)
	if err != nil || !reused {
		t.Fatalf("reuse = %v, %v", reused, err)
	}
	if err := releasecatalog.VerifySelected(destination, catalog, releasecatalog.InstallArtifact); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"windows-assist-backend.exe", "windows-assist-gui.exe", "windowsagent-release.json", "SHA256SUMS"} {
		if _, err := os.Stat(filepath.Join(destination, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected copied file %s: %v", name, err)
		}
	}
	source, _ := os.Stat(filepath.Join(stage, "windows-capture-agent.exe"))
	target, _ := os.Stat(filepath.Join(destination, "windows-capture-agent.exe"))
	if os.SameFile(source, target) {
		t.Fatal("runtime cache must be copied, not linked")
	}
}

func TestStageCachedReleaseLeavesNetworkDestinationAbsentOnMiss(t *testing.T) {
	root := t.TempDir()
	_, catalog := releaseCacheFixture(t, t.TempDir(), "source", "0.1.13")
	destination := filepath.Join(root, "release-staging", "new")
	if reused, err := stageVerifiedCachedRelease(context.Background(), root, destination, catalog); err != nil || reused {
		t.Fatalf("cache miss = %v, %v", reused, err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("network staging requires an absent destination: %v", err)
	}
	releaseCacheFixture(t, root, "complete", "0.1.13")
	if reused, err := stageVerifiedCachedRelease(context.Background(), root, destination, catalog); err != nil || !reused {
		t.Fatalf("cache hit = %v, %v", reused, err)
	}
	if err := releasecatalog.VerifySelected(destination, catalog, releasecatalog.InstallArtifact); err != nil {
		t.Fatal(err)
	}
}

func TestReuseVerifiedReleaseIgnoresIncompleteAndDifferentCatalog(t *testing.T) {
	root := t.TempDir()
	stage, catalog := releaseCacheFixture(t, root, "incomplete", "0.1.13")
	if err := os.Remove(filepath.Join(stage, "windowsagent-release.json")); err != nil {
		t.Fatal(err)
	}
	releaseCacheFixture(t, root, "different", "0.1.12")
	reused, err := reuseVerifiedRelease(context.Background(), root, t.TempDir(), catalog)
	if err != nil || reused {
		t.Fatalf("reuse = %v, %v", reused, err)
	}
}

func TestReuseVerifiedReleaseRejectsNewestCorruptMatch(t *testing.T) {
	for _, name := range []string{"windows-capture-agent.exe", "SHA256SUMS"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			old, _ := releaseCacheFixture(t, root, "old-valid", "0.1.13")
			stage, catalog := releaseCacheFixture(t, root, "new-corrupt", "0.1.13")
			past := time.Now().Add(-time.Hour)
			if err := os.Chtimes(filepath.Join(old, "windowsagent-release.json"), past, past); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, name), []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
			reused, err := reuseVerifiedRelease(context.Background(), root, t.TempDir(), catalog)
			if err == nil || reused {
				t.Fatalf("corrupt matching cache must fail: %v, %v", reused, err)
			}
		})
	}
}

func TestReuseVerifiedReleaseCancellation(t *testing.T) {
	root := t.TempDir()
	_, catalog := releaseCacheFixture(t, root, "complete", "0.1.13")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reused, err := reuseVerifiedRelease(ctx, root, t.TempDir(), catalog)
	if reused || !errors.Is(err, context.Canceled) {
		t.Fatalf("reuse = %v, %v", reused, err)
	}
}
