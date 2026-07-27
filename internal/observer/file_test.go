package observer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFileBackendReadAndRejectEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.dat"), []byte("payload09"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"saves": root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "read", map[string]any{
		"path":   map[string]any{"root": "saves", "relative": "save.dat"},
		"offset": int64(0),
		"length": int64(9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileBytesRead != 9 {
		t.Fatalf("bytes read = %d", result.FileBytesRead)
	}
	if _, err := backend.Call(context.Background(), "file", "stat", map[string]any{
		"path": map[string]any{"root": "saves", "relative": "../escape.dat"},
	}); err == nil {
		t.Fatal("path traversal was accepted")
	}
}

func TestFileBackendOpenBlobCopiesAuthorizedFileWithoutReturningBytes(t *testing.T) {
	root := t.TempDir()
	blobRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.save"), []byte("encoded-save"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackendWithBlobRoot(map[string]string{"saves": root}, blobRoot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "openBlob", map[string]any{
		"path": map[string]any{"root": "saves", "relative": "save.save"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileBytesRead != uint64(len("encoded-save")) {
		t.Fatalf("bytes read = %d", result.FileBytesRead)
	}
	value := result.Value.(map[string]any)
	blob := value["blob"].(map[string]any)
	handle := blob["blobHandle"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(handle) {
		t.Fatalf("handle = %q", handle)
	}
	content, err := os.ReadFile(filepath.Join(blobRoot, handle+".blob"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "encoded-save" {
		t.Fatalf("blob content = %q", content)
	}
	if _, exists := value["data"]; exists {
		t.Fatal("openBlob returned file bytes in the JSON value")
	}
}

func TestFileBackendOpenBlobRequiresHostConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.save"), []byte("encoded-save"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"saves": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Call(context.Background(), "file", "openBlob", map[string]any{
		"path": map[string]any{"root": "saves", "relative": "save.save"},
	}); err == nil {
		t.Fatal("openBlob succeeded without a host-configured blob root")
	}
}
