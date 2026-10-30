package delegatedtask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/piweb"
)

func TestLargeComputerUseEventSurvivesWebSocketJournalAndRestart(t *testing.T) {
	raw := []byte(`{"type":"tool.result","seq":1,"image":"` + strings.Repeat("A", 2<<20) + `"}`)
	var aborts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/machines/local/sessions":
			_, _ = w.Write([]byte(`{"id":"session-1","cwd":"C:\\work"}`))
		case strings.HasSuffix(r.URL.Path, "/prompt"):
			_, _ = w.Write([]byte(`{"accepted":true}`))
		case strings.HasSuffix(r.URL.Path, "/abort"):
			aborts.Add(1)
			_, _ = w.Write([]byte(`{"aborted":true}`))
		case strings.HasSuffix(r.URL.Path, "/events"):
			connection, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.CloseNow()
			for _, payload := range [][]byte{raw, []byte(`{"type":"agent.end","seq":2}`), []byte(`{"type":"status.update","seq":3,"status":{"isStreaming":false,"isCompacting":false,"isBashRunning":false,"pendingMessageCount":0}}`)} {
				if err := connection.Write(r.Context(), websocket.MessageText, payload); err != nil {
					return
				}
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	runtime, err := piweb.New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store, err := eventstream.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	defer store.Close()
	task, err := manager.Submit(context.Background(), SubmitRequest{CWD: `C:\work`, Prompt: "synthetic computer-use result"})
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, task.TaskID, StateCompleted)
	if aborts.Load() != 0 {
		t.Fatal("large event aborted a healthy Pi session")
	}
	events, _, _, err := manager.Events(context.Background(), task.TaskID, 0, 20)
	if err != nil || len(events) != 5 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	var payload struct {
		Artifact EventArtifact `json:"piEventArtifact"`
	}
	if err := json.Unmarshal(events[2].Payload, &payload); err != nil || payload.Artifact.Bytes != len(raw) || payload.Artifact.SHA256 == "" {
		t.Fatalf("payload=%s err=%v", events[2].Payload, err)
	}
	journalBytes, _ := os.ReadFile(filepath.Join(root, eventstream.JournalFilename))
	if len(journalBytes) >= eventstream.MaxEventBytes {
		t.Fatalf("large event was embedded in journal (%d bytes)", len(journalBytes))
	}
	manager.Close()
	store.Close()
	reopened, err := eventstream.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := NewManager(reopened, runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	read, err := recovered.ReadArtifact(context.Background(), task.TaskID, payload.Artifact.ID)
	if err != nil || !bytes.Equal(read, raw) {
		t.Fatalf("artifact read length=%d err=%v", len(read), err)
	}
	if _, err := recovered.ReadArtifact(context.Background(), task.TaskID, "../events.jsonl"); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("invalid artifact ID: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pi-events", task.TaskID, payload.Artifact.ID+".json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.ReadArtifact(context.Background(), task.TaskID, payload.Artifact.ID); err == nil {
		t.Fatal("corrupted artifact returned without a hash failure")
	}
}

func TestLostEventStreamAbortsPiAndPreservesOriginalFailure(t *testing.T) {
	store, _ := eventstream.Open(t.TempDir())
	defer store.Close()
	runtime := newFakeRuntime()
	manager, _ := NewManager(store, runtime)
	defer manager.Close()
	task, err := manager.Submit(context.Background(), SubmitRequest{CWD: `C:\work`, Prompt: "work"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.errors <- errors.New("fixture stream disconnected")
	waitForState(t, manager, task.TaskID, StateFailed)
	if runtime.aborts != 1 {
		t.Fatalf("abort calls=%d", runtime.aborts)
	}
	events, _, _, _ := manager.Events(context.Background(), task.TaskID, 0, 20)
	last := events[len(events)-1]
	if !strings.Contains(string(last.Payload), `"abortConfirmed":true`) || !strings.Contains(string(last.Payload), "PI_WEB_EVENT_STREAM_FAILED") || !strings.Contains(string(last.Payload), "fixture stream disconnected") {
		t.Fatalf("terminal=%s", last.Payload)
	}
}

func TestArtifactCommitFailureIsTerminalAndStopsPi(t *testing.T) {
	root := t.TempDir()
	store, _ := eventstream.Open(root)
	defer store.Close()
	if err := os.WriteFile(filepath.Join(root, "pi-events"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := newFakeRuntime()
	manager, _ := NewManager(store, runtime)
	defer manager.Close()
	task, _ := manager.Submit(context.Background(), SubmitRequest{CWD: `C:\work`, Prompt: "work"})
	runtime.events <- piweb.Event{Type: "tool.result", Seq: 1, Raw: []byte(`{"type":"tool.result","image":"` + strings.Repeat("A", 2<<20) + `"}`)}
	waitForState(t, manager, task.TaskID, StateFailed)
	current, _ := manager.Get(task.TaskID)
	if current.ErrorCode != "PI_WEB_EVENT_ARTIFACT_FAILED" || runtime.aborts != 1 {
		t.Fatalf("task=%+v aborts=%d", current, runtime.aborts)
	}
}

func TestAbortFailureDoesNotClaimPiStopped(t *testing.T) {
	store, _ := eventstream.Open(t.TempDir())
	defer store.Close()
	runtime := newFakeRuntime()
	runtime.abortErr = errors.New("fixture abort rejected")
	manager, _ := NewManager(store, runtime)
	defer manager.Close()
	task, _ := manager.Submit(context.Background(), SubmitRequest{CWD: `C:\work`, Prompt: "work"})
	runtime.errors <- errors.New("fixture stream disconnected")
	waitForState(t, manager, task.TaskID, StateFailed)
	events, _, _, _ := manager.Events(context.Background(), task.TaskID, 0, 20)
	payload := string(events[len(events)-1].Payload)
	if !strings.Contains(payload, `"abortConfirmed":false`) || !strings.Contains(payload, "fixture abort rejected") || !strings.Contains(payload, "PI_WEB_EVENT_STREAM_FAILED") {
		t.Fatalf("terminal=%s", payload)
	}
}

func TestJournalEnvelopeOverheadExternalizesAnOtherwiseSmallPiEvent(t *testing.T) {
	store, _ := eventstream.Open(t.TempDir())
	defer store.Close()
	runtime := newFakeRuntime()
	manager, _ := NewManager(store, runtime)
	defer manager.Close()
	task, _ := manager.Submit(context.Background(), SubmitRequest{CWD: `C:\work`, Prompt: "work"})
	raw := []byte(`{"type":"tool.result","image":"` + strings.Repeat("A", eventstream.MaxEventBytes-100) + `"}`)
	if len(raw) >= eventstream.MaxEventBytes {
		t.Fatal("fixture must fit the former WebSocket limit")
	}
	runtime.events <- piweb.Event{Type: "tool.result", Seq: 1, Raw: raw}
	runtime.events <- piweb.Event{Type: "agent.end", Seq: 2, Raw: []byte(`{"type":"agent.end","seq":2}`)}
	runtime.events <- piweb.Event{Type: "status.update", Seq: 3, Raw: []byte(`{"type":"status.update","status":{"isStreaming":false,"isCompacting":false,"isBashRunning":false,"pendingMessageCount":0}}`)}
	waitForState(t, manager, task.TaskID, StateCompleted)
	events, _, _, _ := manager.Events(context.Background(), task.TaskID, 0, 20)
	if !strings.Contains(string(events[2].Payload), "piEventArtifact") {
		t.Fatalf("payload was not externalized (%d bytes)", len(events[2].Payload))
	}
}
