package observer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fixtureDecoder struct{}

func (fixtureDecoder) Decode(_ context.Context, data []byte, options map[string]any) (any, error) {
	return map[string]any{
		"bytes": len(data),
		"scope": options["scope"],
	}, nil
}

func TestFileBackendReadAndRejectEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.dat"), []byte("inventory"), 0o600); err != nil {
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

func TestFileBackendDecodeUsesRegisteredDecoder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.save"), []byte("encoded-save"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackendWithDecoders(
		map[string]string{"saves": root},
		map[string]FileDecoder{"fixture/inventory": fixtureDecoder{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "decode", map[string]any{
		"path":    map[string]any{"root": "saves", "relative": "save.save"},
		"decoder": "fixture/inventory",
		"options": map[string]any{"scope": "inventory"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileBytesRead != uint64(len("encoded-save")) {
		t.Fatalf("bytes read = %d", result.FileBytesRead)
	}
}

func TestFileBackendDecodeRejectsUnknownDecoder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "save.save"), []byte("encoded-save"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"saves": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Call(context.Background(), "file", "decode", map[string]any{
		"path":    map[string]any{"root": "saves", "relative": "save.save"},
		"decoder": "unregistered/inventory",
	}); err == nil {
		t.Fatal("unknown decoder was accepted")
	}
}
