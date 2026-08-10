package observer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func TestResolveFileRootsUsesPackageKnownFolderDeclaration(t *testing.T) {
	localAppData := t.TempDir()
	roots, err := ResolveFileRoots(&scriptpackage.FilePermissions{
		Roots: []scriptpackage.FileRoot{{
			ID: "game-saves",
			Resolver: scriptpackage.FileRootResolver{
				Kind:        "windows-known-folder",
				KnownFolder: "LocalAppData",
				Relative:    "Publisher/Game/save",
			},
		}},
	}, map[string]string{"LocalAppData": localAppData}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if roots["game-saves"] != filepath.Join(localAppData, "Publisher", "Game", "save") {
		t.Fatalf("roots = %#v", roots)
	}
}

func TestResolveFileRootsUsesSavedGamesDeclaration(t *testing.T) {
	savedGames := t.TempDir()
	roots, err := ResolveFileRoots(&scriptpackage.FilePermissions{
		Roots: []scriptpackage.FileRoot{{
			ID: "elite-dangerous-journal",
			Resolver: scriptpackage.FileRootResolver{
				Kind:        "windows-known-folder",
				KnownFolder: "SavedGames",
				Relative:    "Frontier Developments/Elite Dangerous",
			},
		}},
	}, map[string]string{"SavedGames": savedGames}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(savedGames, "Frontier Developments", "Elite Dangerous")
	if roots["elite-dangerous-journal"] != want {
		t.Fatalf("roots = %#v, want %q", roots, want)
	}
}

func TestResolveFileRootsRejectsMissingKnownFolder(t *testing.T) {
	_, err := ResolveFileRoots(&scriptpackage.FilePermissions{
		Roots: []scriptpackage.FileRoot{{
			ID: "elite-dangerous-journal",
			Resolver: scriptpackage.FileRootResolver{
				Kind:        "windows-known-folder",
				KnownFolder: "SavedGames",
				Relative:    "Frontier Developments/Elite Dangerous",
			},
		}},
	}, map[string]string{"LocalAppData": t.TempDir()}, nil)
	if err == nil {
		t.Fatal("ResolveFileRoots accepted a missing SavedGames path")
	}
}

func TestResolveFileRootsPreservesKnownFolderResolutionError(t *testing.T) {
	want := errors.New("known folder API failed")
	_, err := ResolveFileRoots(&scriptpackage.FilePermissions{
		Roots: []scriptpackage.FileRoot{{
			ID: "elite-dangerous-journal",
			Resolver: scriptpackage.FileRootResolver{
				Kind:        "windows-known-folder",
				KnownFolder: "SavedGames",
				Relative:    "Frontier Developments/Elite Dangerous",
			},
		}},
	}, nil, map[string]error{"SavedGames": want})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped known-folder error", err)
	}
}

func TestFileBackendListReturnsBoundedMetadataWithoutFollowingSymlinks(t *testing.T) {
	root := t.TempDir()
	slot := filepath.Join(root, "account", "slot")
	if err := os.MkdirAll(slot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slot, "save.save"), []byte("save"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(slot, filepath.Join(root, "linked-slot")); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"saves": root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "list", map[string]any{
		"path":       map[string]any{"root": "saves", "relative": "."},
		"maxDepth":   int64(3),
		"maxEntries": int64(16),
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := result.Value.(map[string]any)["entries"].([]map[string]any)
	kinds := map[string]string{}
	for _, entry := range entries {
		kinds[entry["relative"].(string)] = entry["kind"].(string)
	}
	if kinds["account"] != "directory" ||
		kinds["account/slot/save.save"] != "file" ||
		kinds["linked-slot"] != "reparse-point" {
		t.Fatalf("listed kinds = %#v", kinds)
	}
}

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

func TestFileBackendReadJSONReturnsEvidenceAndStrictObject(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Status.json")
	content := []byte(`{"timestamp":"2026-08-09T05:00:51Z","event":"Status","Flags":16}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"elite": root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "readJson", map[string]any{
		"path":     map[string]any{"root": "elite", "relative": "Status.json"},
		"maxBytes": int64(4096),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileBytesRead != uint64(len(content)) {
		t.Fatalf("bytes read = %d", result.FileBytesRead)
	}
	value := result.Value.(map[string]any)
	if value["exists"] != true || value["sourceTimestamp"] != "2026-08-09T05:00:51Z" {
		t.Fatalf("value = %#v", value)
	}
	data := value["data"].(map[string]any)
	if data["event"] != "Status" {
		t.Fatalf("data = %#v", data)
	}
}

func TestFileBackendReadJSONReportsAbsentFile(t *testing.T) {
	root := t.TempDir()
	backend, err := NewFileBackend(map[string]string{"elite": root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "readJson", map[string]any{
		"path":     map[string]any{"root": "elite", "relative": "Market.json"},
		"maxBytes": int64(4096),
	})
	if err != nil {
		t.Fatal(err)
	}
	value := result.Value.(map[string]any)
	if value["exists"] != false || result.FileBytesRead != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFileBackendReadJSONLinesReturnsBoundedTailObjects(t *testing.T) {
	root := t.TempDir()
	content := []byte("{\"event\":\"One\",\"value\":1}\n{\"event\":\"Two\",\"value\":2}\n{\"event\":\"Three\",\"value\":3}\n")
	path := filepath.Join(root, "Journal.log")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewFileBackend(map[string]string{"elite": root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "file", "readJsonLines", map[string]any{
		"path":     map[string]any{"root": "elite", "relative": "Journal.log"},
		"maxBytes": int64(4096), "maxLines": int64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	value := result.Value.(map[string]any)
	items := value["items"].([]map[string]any)
	if len(items) != 2 || items[0]["event"] != "Two" || items[1]["event"] != "Three" {
		t.Fatalf("items=%#v", items)
	}
	if result.FileBytesRead != uint64(len(content)) {
		t.Fatalf("FileBytesRead=%d", result.FileBytesRead)
	}
}

func TestFileBackendReadJSONRejectsMalformedDuplicateAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	backend, err := NewFileBackend(map[string]string{"elite": root})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		content  string
		maxBytes int64
	}{
		{name: "malformed", content: `{"event":`, maxBytes: 4096},
		{name: "duplicate", content: `{"event":"Status","event":"Cargo"}`, maxBytes: 4096},
		{name: "oversized", content: `{"event":"Status"}`, maxBytes: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Call(context.Background(), "file", "readJson", map[string]any{
				"path":     map[string]any{"root": "elite", "relative": test.name + ".json"},
				"maxBytes": test.maxBytes,
			}); err == nil {
				t.Fatal("readJSON accepted invalid input")
			}
		})
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
