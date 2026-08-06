package eventstream

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendReplayAndReopen(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC) }
	store.random = strings.NewReader("0123456789abcdef")

	first, err := store.Append(context.Background(), testAppendRequest())
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || first.EventID != "evt_30313233343536373839616263646566" {
		t.Fatalf("unexpected first event: %#v", first)
	}
	secondRequest := testAppendRequest()
	secondRequest.Type = "screenparser.change"
	store.random = strings.NewReader("fedcba9876543210")
	second, err := store.Append(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 {
		t.Fatalf("second sequence = %d", second.Sequence)
	}

	events, err := store.ReadAfter(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "screenparser.change" {
		t.Fatalf("unexpected replay: %#v", events)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	last, err := reopened.LastSequence()
	if err != nil {
		t.Fatal(err)
	}
	if last != 2 {
		t.Fatalf("last sequence = %d", last)
	}
}

func TestAppendRejectsInvalidPayloadWithoutWriting(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	request := testAppendRequest()
	request.Payload = json.RawMessage(`{"duplicate":1,"duplicate":2}`)
	if _, err := store.Append(context.Background(), request); err == nil {
		t.Fatal("expected duplicate payload key to fail")
	}
	last, err := store.LastSequence()
	if err != nil {
		t.Fatal(err)
	}
	if last != 0 {
		t.Fatalf("last sequence = %d", last)
	}
}

func TestReadRejectsCursorAhead(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.ReadAfter(context.Background(), 1, 10)
	if !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("error = %v", err)
	}
}

func TestWaitAfterUnblocksOnCommittedEvent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := make(chan []Event, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		events, err := store.WaitAfter(context.Background(), 0, 10)
		if err != nil {
			errorsChannel <- err
			return
		}
		result <- events
	}()
	if _, err := store.Append(context.Background(), testAppendRequest()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorsChannel:
		t.Fatal(err)
	case events := <-result:
		if len(events) != 1 || events[0].Sequence != 1 {
			t.Fatalf("events = %+v", events)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitAfter did not unblock")
	}
}

func TestWriteFailurePoisonsStoreAndWakesWaiter(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	waitError := make(chan error, 1)
	go func() {
		_, err := store.WaitAfter(context.Background(), 0, 10)
		waitError <- err
	}()
	if err := store.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), testAppendRequest()); err == nil {
		t.Fatal("expected append to closed journal file to fail")
	}
	select {
	case err := <-waitError:
		if err == nil || !strings.Contains(err.Error(), "unavailable after a write failure") {
			t.Fatalf("wait error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not wake after journal write failure")
	}
	if _, err := store.LastSequence(); err == nil || !strings.Contains(err.Error(), "unavailable after a write failure") {
		t.Fatalf("last sequence error = %v", err)
	}
}

func TestOpenRejectsUnterminatedRecord(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, JournalFilename), []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(root)
	if err == nil || !strings.Contains(err.Error(), "not newline terminated") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenRejectsSequenceGap(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	store.random = strings.NewReader("0123456789abcdef")
	event, err := store.Append(context.Background(), testAppendRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	event.Sequence = 3
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(root, JournalFilename), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(root)
	if err == nil || !strings.Contains(err.Error(), "expected 2") {
		t.Fatalf("error = %v", err)
	}
}

func testAppendRequest() AppendRequest {
	return AppendRequest{
		SessionID:  "session_20260806",
		Stream:     "screen/ui",
		Type:       "screenparser.snapshot",
		ObservedAt: time.Date(2026, 8, 6, 1, 2, 2, 0, time.UTC),
		Source: Source{
			ModuleID:   "screen/ui",
			InstanceID: "module_1",
			Runtime:    "screenparser-v2",
		},
		Foreground: Foreground{
			ExecutableName: "EliteDangerous64.exe",
			Revision:       1,
		},
		Payload: json.RawMessage(`{"elements":[]}`),
	}
}
