package observationjob

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotPackageIsUnaffectedBySourceReplacement(t *testing.T) {
	source := filepath.Join(t.TempDir(), "package")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(source, "main.star")
	if err := os.WriteFile(sourceFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := snapshotPackage(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(snapshot)) })
	if err := os.WriteFile(sourceFile, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(snapshot, "main.star"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("snapshot content = %q, want old", content)
	}
}

func TestSnapshotPackageRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "package")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "main.star")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if snapshot, err := snapshotPackage(source); err == nil {
		os.RemoveAll(filepath.Dir(snapshot))
		t.Fatal("snapshotPackage accepted a symlink")
	}
}
