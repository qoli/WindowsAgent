package scenereducer

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventhttp"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

func TestRunnerProcessCommitsOutputAndDurableCursor(t *testing.T) {
	config := testConfig()
	store, client, closeServer := testEventClient(t)
	defer closeServer()
	input := appendInput(t, store, parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{testDetection("Button", .9, .1, .1, .2, .2)}))
	statePath := filepath.Join(t.TempDir(), "state.json")
	state := InitialState(config)
	if err := SaveState(statePath, state, config); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Config: config, Client: client, StatePath: statePath}
	state, err := runner.process(context.Background(), state, input)
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != 1 || state.LastOutputSequence != 2 || state.Pending != nil {
		t.Fatalf("state = %+v", state)
	}
	reloaded, err := LoadState(statePath, config)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Cursor != state.Cursor || reloaded.LastOutputSequence != state.LastOutputSequence {
		t.Fatalf("reloaded state = %+v", reloaded)
	}
	events, err := store.ReadAfter(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != config.Output.SceneChangedType || events[0].CausationID != input.EventID {
		t.Fatalf("output events = %+v", events)
	}
}

func TestRunnerRecoversCommittedPendingOutputWithoutDuplicate(t *testing.T) {
	config := testConfig()
	store, client, closeServer := testEventClient(t)
	defer closeServer()
	input := appendInput(t, store, parsedEvent(t, 1, time.Unix(100, 0).UTC(), []Detection{testDetection("Button", .9, .1, .1, .2, .2)}))
	reduction, err := Reduce(config, InitialState(config), input)
	if err != nil {
		t.Fatal(err)
	}
	state := InitialState(config)
	state.Pending = &PendingOutput{InputSequence: input.Sequence, InputEventID: input.EventID, Request: *reduction.Request, Next: reduction.Next}
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := SaveState(statePath, state, config); err != nil {
		t.Fatal(err)
	}
	committed, err := client.Append(context.Background(), *reduction.Request)
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{Config: config, Client: client, StatePath: statePath}
	recovered, err := runner.recoverPending(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	last, err := store.LastSequence()
	if err != nil {
		t.Fatal(err)
	}
	if last != committed.Sequence || recovered.LastOutputSequence != committed.Sequence || recovered.Pending != nil {
		t.Fatalf("last=%d committed=%d state=%+v", last, committed.Sequence, recovered)
	}
}

func testEventClient(t *testing.T) (*eventstream.Store, *Client, func()) {
	t.Helper()
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	api, err := eventhttp.New(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	client, err := NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	return store, client, func() { server.Close(); store.Close() }
}

func appendInput(t *testing.T, store *eventstream.Store, source eventstream.Event) eventstream.Event {
	t.Helper()
	request := eventstream.AppendRequest{
		SessionID: source.SessionID, Stream: source.Stream, Type: source.Type, ObservedAt: source.ObservedAt,
		Source: source.Source, Foreground: source.Foreground, Payload: source.Payload,
	}
	event, err := store.Append(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
