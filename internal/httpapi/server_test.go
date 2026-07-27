package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/qoli/WindowsAgent/internal/artifact"
	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

type fakeCapturer struct {
	status      capture.Status
	result      capture.Result
	statusError error
	captureErr  error
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (f *fakeCapturer) Status(context.Context) (capture.Status, error) {
	return f.status, f.statusError
}

func (f *fakeCapturer) Capture(ctx context.Context, includeCursor bool) (capture.Result, error) {
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return capture.Result{}, ctx.Err()
		}
	}
	if f.captureErr != nil {
		return capture.Result{}, f.captureErr
	}
	result := f.result
	result.IncludeCursor = includeCursor
	return result, nil
}

func TestCaptureAndDownload(t *testing.T) {
	server, _ := newTestServer(t, &fakeCapturer{status: testStatus(), result: testResult()})
	request := httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":false}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata artifact.Metadata
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.IncludeCursor {
		t.Fatal("include_cursor was not propagated")
	}
	if metadata.Foreground.ExecutableName != "game.exe" || metadata.Foreground.ProcessID != 42 {
		t.Fatalf("foreground process metadata = %+v", metadata.Foreground)
	}
	if metadata.Rule.Status != rules.StatusMatched || metadata.Rule.ID != "game.exe" || metadata.Rule.Agents == nil {
		t.Fatalf("rule navigation = %+v", metadata.Rule)
	}
	if metadata.Rule.Description != rules.MatchedDescription {
		t.Fatalf("rule description = %q", metadata.Rule.Description)
	}

	agentsRequest := httptest.NewRequest(http.MethodGet, metadata.Rule.Agents.URL, nil)
	agentsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(agentsResponse, agentsRequest)
	if agentsResponse.Code != http.StatusOK {
		t.Fatalf("AGENTS status = %d, body = %s", agentsResponse.Code, agentsResponse.Body.String())
	}
	if agentsResponse.Header().Get("Content-Type") != "text/markdown; charset=utf-8" ||
		agentsResponse.Header().Get("ETag") != `"`+metadata.Rule.Agents.SHA256+`"` ||
		agentsResponse.Body.String() != "# Game guidance\n" {
		t.Fatal("AGENTS response does not match capture navigation")
	}

	contentRequest := httptest.NewRequest(http.MethodGet, metadata.ContentURL, nil)
	contentResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusOK {
		t.Fatalf("content status = %d", contentResponse.Code)
	}
	if contentResponse.Header().Get("ETag") != `"`+metadata.SHA256+`"` {
		t.Fatal("missing or invalid ETag")
	}
	if !bytes.Equal(contentResponse.Body.Bytes(), testResult().PNG) {
		t.Fatal("downloaded content differs from capture")
	}
}

func TestCaptureReportsUnmatchedRuleExplicitly(t *testing.T) {
	result := testResult()
	result.Foreground.ExecutableName = "explorer.exe"
	result.Foreground.ExecutablePath = `C:\Windows\explorer.exe`
	server, _ := newTestServer(t, &fakeCapturer{status: testStatus(), result: result})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":true}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata artifact.Metadata
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Rule.Status != rules.StatusUnmatched ||
		metadata.Rule.Description != rules.UnmatchedDescription ||
		metadata.Rule.ID != "" ||
		metadata.Rule.Agents != nil {
		t.Fatalf("rule navigation = %+v", metadata.Rule)
	}
}

func TestRuleDocumentRejectsUnknownAndNonCanonicalIDs(t *testing.T) {
	server, _ := newTestServer(t, &fakeCapturer{status: testStatus(), result: testResult()})
	for _, path := range []string{
		"/v1/rules/unknown.exe/AGENTS.md",
		"/v1/rules/GAME.EXE/AGENTS.md",
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("path %q status = %d, want 404", path, response.Code)
		}
		assertErrorCode(t, response.Body.Bytes(), "rule_not_found")
	}
}

func TestCaptureRequestIsStrict(t *testing.T) {
	server, _ := newTestServer(t, &fakeCapturer{status: testStatus(), result: testResult()})
	tests := []string{
		`{}`,
		`{"include_cursor":true,"unknown":1}`,
		`{"include_cursor":true} {}`,
	}
	for _, body := range tests {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, response.Code)
		}
		assertErrorCode(t, response.Body.Bytes(), "invalid_request")
	}
}

func TestConcurrentCaptureReturnsBusy(t *testing.T) {
	fake := &fakeCapturer{
		status:  testStatus(),
		result:  testResult(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	server, _ := newTestServer(t, fake)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":true}`)))
		firstDone <- response
	}()
	<-fake.started

	second := httptest.NewRecorder()
	server.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":true}`)))
	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", second.Code)
	}
	assertErrorCode(t, second.Body.Bytes(), "capture_busy")
	close(fake.release)
	if response := <-firstDone; response.Code != http.StatusCreated {
		t.Fatalf("first capture status = %d", response.Code)
	}
}

func TestCaptureTimeoutHasStableError(t *testing.T) {
	fake := &fakeCapturer{
		status:  testStatus(),
		result:  testResult(),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	server, _ := newTestServerWithTimeout(t, fake, 10*time.Millisecond)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":true}`)))
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", response.Code)
	}
	assertErrorCode(t, response.Body.Bytes(), "capture_timeout")
}

func TestCaptureFailureDoesNotCreateArtifact(t *testing.T) {
	fake := &fakeCapturer{
		status:     testStatus(),
		captureErr: capture.Failure("desktop_unavailable", "interactive desktop is unavailable", errors.New("locked")),
	}
	server, store := newTestServer(t, fake)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/captures", bytes.NewBufferString(`{"include_cursor":true}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	assertErrorCode(t, response.Body.Bytes(), "desktop_unavailable")
	count, err := store.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("artifact count = %d, want 0", count)
	}
}

func TestLatestMissingReturnsStableError(t *testing.T) {
	server, _ := newTestServer(t, &fakeCapturer{status: testStatus(), result: testResult()})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/captures/latest", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	assertErrorCode(t, response.Body.Bytes(), "artifact_not_found")
}

func newTestServer(t *testing.T, capturer capture.Capturer) (*Server, *artifact.Store) {
	t.Helper()
	return newTestServerWithTimeout(t, capturer, time.Second)
}

func newTestServerWithTimeout(t *testing.T, capturer capture.Capturer, timeout time.Duration) (*Server, *artifact.Store) {
	t.Helper()
	store, err := artifact.New(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ruleRegistry, err := rules.New(fstest.MapFS{
		"game.exe/AGENTS.md": &fstest.MapFile{Data: []byte("# Game guidance\n"), Mode: fs.FileMode(0o444)},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(capturer, store, ruleRegistry, timeout, "test", logger)
	if err != nil {
		t.Fatal(err)
	}
	return server, store
}

func testStatus() capture.Status {
	return capture.Status{
		Supported: true,
		Monitor: capture.Monitor{
			DeviceName: "\\\\.\\DISPLAY1",
			Width:      1920,
			Height:     1080,
			ColorSpace: "RGB_FULL_G22_NONE_P709",
		},
	}
}

func testResult() capture.Result {
	var encoded bytes.Buffer
	imageValue := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	imageValue.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&encoded, imageValue); err != nil {
		panic(err)
	}
	monitor := testStatus().Monitor
	monitor.Width = 1
	monitor.Height = 1
	return capture.Result{
		PNG:     encoded.Bytes(),
		Width:   1,
		Height:  1,
		Monitor: monitor,
		Foreground: foreground.Info{
			ObservedAt:     time.Date(2026, 7, 27, 1, 2, 3, 4, time.UTC),
			ProcessID:      42,
			ExecutableName: "game.exe",
			ExecutablePath: `C:\Games\game.exe`,
			WindowTitle:    "Game",
		},
		CapturePixelFormat: "B8G8R8A8_UNORM",
	}
}

func assertErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var envelope ErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != want {
		t.Fatalf("error code = %q, want %q", envelope.Error.Code, want)
	}
}
