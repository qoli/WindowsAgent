package visuallog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCaptureClientRequiresMatchedTargetAndVerifiesContent(t *testing.T) {
	content := []byte("jpeg-content")
	sum := sha256.Sum256(content)
	observedAt := time.Date(2026, 8, 11, 2, 3, 4, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/captures":
			if r.Method != http.MethodPost {
				t.Fatalf("capture method = %s", r.Method)
			}
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request["profile"] != "1080p-jpeg" || request["include_cursor"] != false {
				t.Fatalf("capture request = %#v", request)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "cap_test", "created_at": observedAt.Add(time.Second), "profile": "1080p-jpeg",
				"content_type": "image/jpeg", "bytes": len(content), "sha256": hex.EncodeToString(sum[:]),
				"foreground": map[string]any{
					"observed_at": observedAt, "process_id": 42, "executable_name": "EliteDangerous64.exe",
					"executable_path": `C:\Games\EliteDangerous64.exe`,
				},
				"rule":        map[string]any{"status": "matched", "description": "Elite.", "id": "EliteDangerous64.exe"},
				"content_url": "/v1/captures/cap_test/content",
			})
		case "/v1/captures/cap_test/content":
			_, _ = w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewCaptureClient(server.URL, server.Client(), "1080p-jpeg", "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	frame, err := client.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frame.CaptureID != "cap_test" || frame.ObservedAt != observedAt || string(frame.Content) != string(content) || frame.ForegroundRevision != 1 {
		t.Fatalf("frame = %+v", frame)
	}
}

func TestCaptureClientRejectsForegroundDrift(t *testing.T) {
	content := []byte("jpeg")
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "cap_test", "created_at": now, "profile": "1080p-jpeg", "content_type": "image/jpeg",
			"bytes": len(content), "sha256": hex.EncodeToString(sum[:]),
			"foreground":  map[string]any{"observed_at": now, "process_id": 7, "executable_name": "Notepad.exe", "executable_path": `C:\Windows\Notepad.exe`},
			"rule":        map[string]any{"status": "unmatched", "description": "No rule guidance is available for this foreground process."},
			"content_url": "/v1/captures/cap_test/content",
		})
	}))
	defer server.Close()
	client, err := NewCaptureClient(server.URL, server.Client(), "1080p-jpeg", "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Capture(context.Background())
	if err == nil || !strings.Contains(err.Error(), "foreground Rule is not EliteDangerous64.exe") {
		t.Fatalf("error = %v", err)
	}
}
