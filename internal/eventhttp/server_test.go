package eventhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestAuthenticatedAppendAndReplay(t *testing.T) {
	server := testServer(t)
	body, err := json.Marshal(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	appendRequest := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	appendRequest.Header.Set("Content-Type", "application/json")
	appendRequest.Header.Set("Authorization", "Bearer "+testToken)
	appendResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(appendResponse, appendRequest)
	if appendResponse.Code != http.StatusCreated {
		t.Fatalf("append status = %d, body = %s", appendResponse.Code, appendResponse.Body.String())
	}

	replayRequest := httptest.NewRequest(http.MethodGet, "/v1/events?after=0&limit=1", nil)
	replayRequest.Header.Set("Authorization", "Bearer "+testToken)
	replayRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayRecorder, replayRequest)
	if replayRecorder.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body = %s", replayRecorder.Code, replayRecorder.Body.String())
	}
	var replay replayResponse
	if err := json.Unmarshal(replayRecorder.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if len(replay.Events) != 1 || replay.NextCursor != 1 || replay.LastSequence != 1 {
		t.Fatalf("replay = %+v", replay)
	}
}

func TestAuthenticatedReplayFiltersByStreamAndAdvancesCursor(t *testing.T) {
	server := testServer(t)
	for _, stream := range []string{"screen/ui", "action.runs", "screen/ui"} {
		request := testRequest()
		request.Stream = stream
		body, _ := json.Marshal(request)
		httpRequest := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("Authorization", "Bearer "+testToken)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httpRequest)
		if response.Code != http.StatusCreated {
			t.Fatalf("append status = %d body=%s", response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/events?after=0&limit=10&stream=action.runs", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var replay replayResponse
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if len(replay.Events) != 1 || replay.Events[0].Sequence != 2 || replay.NextCursor != 3 || replay.LastSequence != 3 {
		t.Fatalf("replay = %+v", replay)
	}
}

func TestEventAPIRejectsMissingAuthentication(t *testing.T) {
	server := testServer(t)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/events?after=0", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestEventAPIRejectsCursorAhead(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/events?after=1", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("event_cursor_ahead")) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEventAPIRejectsExplicitZeroReplayLimit(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/events?after=0&limit=0", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("limit must be positive")) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEventAPIRejectsInvalidReplayStream(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/events?after=0&stream=not%20canonical", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("invalid_replay_request")) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEventAPIReturnsAuthenticatedTimeRange(t *testing.T) {
	server := testServer(t)
	for index := 0; index < 3; index++ {
		request := testRequest()
		request.Stream = "visual-log"
		request.Type = "visual-log.observation"
		request.ObservedAt = time.Date(2026, 8, 11, 1, 2, index, 0, time.UTC)
		body, _ := json.Marshal(request)
		httpRequest := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("Authorization", "Bearer "+testToken)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httpRequest)
		if response.Code != http.StatusCreated {
			t.Fatalf("append status = %d body=%s", response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet,
		"/v1/events/range?from=2026-08-11T01%3A02%3A00Z&to=2026-08-11T01%3A02%3A03Z&stream=visual-log&limit=2", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("range status = %d body=%s", response.Code, response.Body.String())
	}
	var result timeRangeResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Complete || result.NextCursor != 2 || result.LastSequence != 3 {
		t.Fatalf("range result = %+v", result)
	}
}

func TestEventAPIRejectsNonUTCTimeRange(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodGet,
		"/v1/events/range?from=2026-08-11T09%3A00%3A00%2B08%3A00&to=2026-08-11T10%3A00%3A00%2B08%3A00&stream=visual-log", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "UTC") {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestEventAPIRejectsUnknownBodyField(t *testing.T) {
	server := testServer(t)
	body := []byte(`{"sessionId":"session_1","unknown":true}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestEventStreamDeliversCommittedEvent(t *testing.T) {
	server := testServer(t)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events/stream?after=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("status = %d, content type = %q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	body, err := json.Marshal(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	appendRequest, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/events", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	appendRequest.Header.Set("Content-Type", "application/json")
	appendRequest.Header.Set("Authorization", "Bearer "+testToken)
	appendResponse, err := http.DefaultClient.Do(appendRequest)
	if err != nil {
		t.Fatal(err)
	}
	appendResponse.Body.Close()
	if appendResponse.StatusCode != http.StatusCreated {
		t.Fatalf("append status = %d", appendResponse.StatusCode)
	}

	line, err := bufio.NewReader(response.Body).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var event eventstream.Event
	if err := json.Unmarshal(line, &event); err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 1 || event.Type != "screenparser.snapshot" {
		t.Fatalf("event = %+v", event)
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	store, err := eventstream.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, err := New(store, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func testRequest() eventstream.AppendRequest {
	return eventstream.AppendRequest{
		SessionID:  "session_1",
		Stream:     "screen/ui",
		Type:       "screenparser.snapshot",
		ObservedAt: time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC),
		Source: eventstream.Source{
			ModuleID:   "screen/ui",
			InstanceID: "module_1",
			Runtime:    "screenparser-v2",
		},
		Foreground: eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1},
		Payload:    json.RawMessage(`{"elements":[]}`),
	}
}
