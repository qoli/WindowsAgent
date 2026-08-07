package artifact

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

func TestCommitLatestAndReadContent(t *testing.T) {
	store := newTestStore(t, 100)
	result := testResult("first")
	metadata, err := store.Commit(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ContentURL != "/v1/captures/"+metadata.ID+"/content" {
		t.Fatalf("unexpected content URL: %s", metadata.ContentURL)
	}
	if metadata.Rule.Status != rules.StatusMatched || metadata.Rule.ID != "game.exe" {
		t.Fatalf("unexpected rule navigation: %+v", metadata.Rule)
	}

	latest, err := store.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != metadata.ID {
		t.Fatalf("latest ID = %s, want %s", latest.ID, metadata.ID)
	}

	readMetadata, content, err := store.ReadContent(context.Background(), metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readMetadata.SHA256 != metadata.SHA256 || !bytes.Equal(content, result.PNG) {
		t.Fatal("read artifact did not match committed artifact")
	}
}

func TestRetentionKeepsNewestArtifacts(t *testing.T) {
	store := newTestStore(t, 2)
	var ids []string
	current := time.Date(2026, 7, 25, 1, 2, 3, 4, time.UTC)
	store.now = func() time.Time { return current }
	for i := 0; i < 3; i++ {
		metadata, err := store.Commit(context.Background(), testResult(string(rune('a'+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, metadata.ID)
		current = current.Add(time.Second)
	}
	count, err := store.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if _, err := store.Get(context.Background(), ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest artifact error = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(context.Background(), ids[2]); err != nil {
		t.Fatalf("newest artifact missing: %v", err)
	}
}

func TestCorruptMetadataFailsExplicitly(t *testing.T) {
	store := newTestStore(t, 100)
	metadata, err := store.Commit(context.Background(), testResult("bad"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, metadata.ID, metadataFilename)
	if err := os.WriteFile(path, []byte(`{"id":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Count(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("count error = %v, want ErrCorrupt", err)
	}
}

func TestStagingDirectoryFailsExplicitly(t *testing.T) {
	store := newTestStore(t, 100)
	if err := os.Mkdir(filepath.Join(store.root, ".staging-incomplete"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Count(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("count error = %v, want ErrCorrupt", err)
	}
}

func TestCommitRejectsMissingProvenance(t *testing.T) {
	store := newTestStore(t, 100)
	result := testResult("invalid")
	result.Monitor.DeviceName = ""
	if _, err := store.Commit(context.Background(), result); err == nil {
		t.Fatal("expected missing monitor provenance error")
	}
}

func TestCommitRejectsMissingForegroundProcess(t *testing.T) {
	store := newTestStore(t, 100)
	result := testResult("invalid")
	result.Foreground = foreground.Info{}
	if _, err := store.Commit(context.Background(), result); err == nil {
		t.Fatal("expected missing foreground process error")
	}
}

func TestCommitRejectsMissingRuleResolution(t *testing.T) {
	store := newTestStore(t, 100)
	result := testResult("invalid")
	result.Rule = rules.Resolution{}
	if _, err := store.Commit(context.Background(), result); err == nil {
		t.Fatal("expected missing rule resolution error")
	}
}

func newTestStore(t *testing.T, retention int) *Store {
	t.Helper()
	store, err := New(t.TempDir(), retention)
	if err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 7, 25, 1, 2, 3, 4, time.UTC)
	store.now = func() time.Time { return current }
	store.random = bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4}, 1024))
	return store
}

func testResult(content string) capture.Result {
	var encoded bytes.Buffer
	value := uint8(len(content))
	imageValue := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	imageValue.SetNRGBA(0, 0, color.NRGBA{R: value, G: 20, B: 30, A: 255})
	if err := png.Encode(&encoded, imageValue); err != nil {
		panic(err)
	}
	return capture.Result{
		PNG:           encoded.Bytes(),
		Width:         1,
		Height:        1,
		IncludeCursor: true,
		Monitor: capture.Monitor{
			DeviceName: "\\\\.\\DISPLAY1",
			Width:      1,
			Height:     1,
			ColorSpace: "RGB_FULL_G22_NONE_P709",
		},
		Foreground: foreground.Info{
			ObservedAt:     time.Date(2026, 7, 25, 1, 2, 3, 4, time.UTC),
			ProcessID:      42,
			ExecutableName: "game.exe",
			ExecutablePath: `C:\Games\game.exe`,
			WindowTitle:    "Game",
		},
		Rule: rules.Resolution{
			Status:      rules.StatusMatched,
			Description: "Read the live Rule before acting.",
			ID:          "game.exe",
			Agents: &rules.Document{
				URL:         "/v1/rules/game.exe/AGENTS.md",
				ContentType: "text/markdown; charset=utf-8",
			},
			Scripts: &rules.Document{
				URL:         "/v1/rules/game.exe/scripts",
				ContentType: "application/json; charset=utf-8",
			},
			Actions: &rules.Document{
				URL:         "/v3/rules/game.exe/actions",
				ContentType: "application/json; charset=utf-8",
			},
			Registrations: &rules.Document{
				URL:         "/v3/rules/game.exe/registrations",
				ContentType: "application/json; charset=utf-8",
			},
			Runtimes: &rules.Document{
				URL:         "/v4/rules/game.exe/runtimes",
				ContentType: "application/json; charset=utf-8",
			},
		},
		CapturePixelFormat: "B8G8R8A8_UNORM",
	}
}
