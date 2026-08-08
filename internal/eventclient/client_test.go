package eventclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventhttp"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

func TestClientAppendsAndStreamsAuthenticatedEvents(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := "01234567890123456789012345678901"
	api, err := eventhttp.New(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "event.token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(server.URL, tokenFile, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := eventstream.AppendRequest{
		SessionID: "act_1", Stream: "action.runs", Type: "action.started", ObservedAt: time.Now().UTC(),
		Source:     eventstream.Source{ModuleID: "game/action", InstanceID: "act_1", Runtime: "fixture-v1"},
		Foreground: eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1}, CorrelationID: "act_1",
		Payload: json.RawMessage(`{"state":"RUNNING"}`),
	}
	committed, err := client.Append(context.Background(), request)
	if err != nil || committed.Sequence != 1 {
		t.Fatalf("committed = %+v, err = %v", committed, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errSeen := errors.New("seen")
	err = client.Stream(ctx, 0, func(event eventstream.Event) error {
		if event.Sequence != 1 || event.CorrelationID != "act_1" {
			t.Fatalf("event = %+v", event)
		}
		return errSeen
	})
	if !errors.Is(err, errSeen) {
		t.Fatalf("stream error = %v", err)
	}
}

func TestClientRejectsNonLoopbackAndMalformedToken(t *testing.T) {
	client := &http.Client{}
	if _, err := New("http://192.0.2.1:8788", filepath.Join(t.TempDir(), "missing"), client); err == nil {
		t.Fatal("non-loopback event URL was accepted")
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New("http://127.0.0.1:8788", tokenFile, client); err == nil {
		t.Fatal("short event token was accepted")
	}
}

func TestClientReadsCurrentLastSequence(t *testing.T) {
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := "01234567890123456789012345678901"
	api, err := eventhttp.New(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "event.token")
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(server.URL, tokenFile, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	request := eventstream.AppendRequest{
		SessionID: "act_1", Stream: "action.runs", Type: "action.started", ObservedAt: time.Now().UTC(),
		Source:     eventstream.Source{ModuleID: "game/action", InstanceID: "act_1", Runtime: "fixture-v1"},
		Foreground: eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1}, CorrelationID: "act_1",
		Payload: json.RawMessage(`{"state":"RUNNING"}`),
	}
	if _, err := store.Append(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	last, err := client.LastSequence(context.Background())
	if err != nil || last != 1 {
		t.Fatalf("last=%d error=%v", last, err)
	}
	events, next, replayLast, err := client.Replay(context.Background(), 0, 1)
	if err != nil || len(events) != 1 || events[0].Sequence != 1 || next != 1 || replayLast != 1 {
		t.Fatalf("events=%+v next=%d last=%d error=%v", events, next, replayLast, err)
	}
}
