package assistgui

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotReportsHealthAndDisabledTailscale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"windows-capture-agent","version":"test-version","listen":"0.0.0.0:8787"}`))
	}))
	defer server.Close()
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "bin", "windows-capture-agent.exe"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspector := Inspector{DataDir: dataDir, AgentURL: server.URL, Port: "8787", Interfaces: func() ([]net.Interface, error) { return nil, nil }}
	snapshot, err := inspector.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Installed || !snapshot.AgentHealthy || snapshot.AgentVersion != "test-version" {
		t.Fatalf("unexpected agent snapshot: %#v", snapshot)
	}
	if snapshot.Tailscale.Enabled || snapshot.Tailscale.State != TailscaleDisabled {
		t.Fatalf("unexpected Tailscale snapshot: %#v", snapshot.Tailscale)
	}
}

func TestSnapshotRejectsAnotherServiceOnTheAgentPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","service":"other","version":"test-version","listen":"0.0.0.0:8787"}`))
	}))
	defer server.Close()
	snapshot, err := (Inspector{DataDir: t.TempDir(), AgentURL: server.URL, Port: "8787", Interfaces: func() ([]net.Interface, error) { return nil, nil }}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentHealthy || !strings.Contains(snapshot.AgentHealthError, "health service") {
		t.Fatalf("unexpected agent snapshot: %#v", snapshot)
	}
}

func TestSnapshotPreservesHealthFailure(t *testing.T) {
	dataDir := t.TempDir()
	inspector := Inspector{DataDir: dataDir, AgentURL: "http://127.0.0.1:1/healthz", Port: "8787", Interfaces: func() ([]net.Interface, error) { return nil, nil }}
	snapshot, err := inspector.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentHealthy || snapshot.AgentHealthError == "" {
		t.Fatalf("unexpected health snapshot: %#v", snapshot)
	}
}

func TestLoadTailscaleRejectsMalformedState(t *testing.T) {
	dataDir := t.TempDir()
	directory := filepath.Join(dataDir, "tailscale")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "status.json"), []byte(`{"schemaVersion":1,"enabled":true,"state":"MAGIC","updatedAt":"2026-09-18T00:00:00Z","processId":42,"generation":"0123456789abcdef0123456789abcdef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	status := LoadTailscaleStatus(dataDir)
	if status.State != TailscaleFailed || !strings.Contains(status.Error, "invalid adapter state") {
		t.Fatalf("LoadTailscaleStatus() = %#v", status)
	}
}
