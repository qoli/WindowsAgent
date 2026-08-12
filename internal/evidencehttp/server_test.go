package evidencehttp

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/evidence"
	"github.com/qoli/WindowsAgent/internal/videocapture"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeDecoder struct{}

func (fakeDecoder) Decode(_ context.Context, _ string, offsets []time.Duration, emit func(time.Duration, image.Image) error) error {
	for _, offset := range offsets {
		if err := emit(offset, image.NewRGBA(image.Rect(0, 0, 1920, 1080))); err != nil {
			return err
		}
	}
	return nil
}

type fakeRunControl struct {
	status  evidence.RunStatus
	runs    map[string]evidence.RunStatus
	request evidence.RunRequest
	start   evidence.RunStatus
	err     error
}

func newFakeRunControl() *fakeRunControl {
	return &fakeRunControl{
		status: evidence.RunStatus{State: evidence.RunIdle, Finite: true, DefaultDurationSeconds: 1200, MaxDurationSeconds: 1200},
		runs:   make(map[string]evidence.RunStatus),
	}
}

func (c *fakeRunControl) Start(request evidence.RunRequest) (evidence.RunStatus, error) {
	c.request = request
	if request.DurationSeconds != nil && (*request.DurationSeconds < 1 || *request.DurationSeconds > evidence.MaxRunDurationSeconds) {
		return c.status, evidence.ErrDurationInvalid
	}
	if c.err != nil {
		return c.start, c.err
	}
	if c.start.RunID == "" {
		requestedAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
		endsAt := requestedAt.Add(20 * time.Minute)
		c.start = evidence.RunStatus{State: evidence.RunStarting, RunID: "evr_test", Finite: true, DefaultDurationSeconds: 1200, MaxDurationSeconds: 1200, DurationSeconds: 1200, RequestedAt: &requestedAt, EndsAt: &endsAt}
	}
	c.status = c.start
	c.runs[c.start.RunID] = c.start
	return c.start, nil
}

func TestFiniteRunAPIUsesDefaultDurationAndReturnsAddressableStatus(t *testing.T) {
	control := newFakeRunControl()
	server, err := New(openStore(t), fakeDecoder{}, testToken, time.Minute, control)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/evidence/runs", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || response.Header().Get("Location") != "/v1/evidence/runs/evr_test" {
		t.Fatalf("status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"finite":true`) || !strings.Contains(response.Body.String(), `"durationSeconds":1200`) || !strings.Contains(response.Body.String(), `"endsAt":`) {
		t.Fatalf("finite contract missing: %s", response.Body.String())
	}
	if control.request.DurationSeconds != nil {
		t.Fatalf("default duration should remain omitted at the HTTP seam: %+v", control.request)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/evidence/runs/evr_test", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"runId":"evr_test"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFiniteRunAPIAcceptsOnlyOptionalBoundedIntegerDuration(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		code int
	}{
		{name: "explicit duration", body: `{"durationSeconds":37}`, code: http.StatusAccepted},
		{name: "zero", body: `{"durationSeconds":0}`, code: http.StatusBadRequest},
		{name: "over maximum", body: `{"durationSeconds":1201}`, code: http.StatusBadRequest},
		{name: "fraction", body: `{"durationSeconds":1.5}`, code: http.StatusBadRequest},
		{name: "null", body: `{"durationSeconds":null}`, code: http.StatusBadRequest},
		{name: "unknown", body: `{"durationSeconds":20,"extend":true}`, code: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := newFakeRunControl()
			server, _ := New(openStore(t), fakeDecoder{}, testToken, time.Minute, control)
			request := httptest.NewRequest(http.MethodPost, "/v1/evidence/runs", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+testToken)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.code {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.code == http.StatusAccepted && (control.request.DurationSeconds == nil || *control.request.DurationSeconds != 37) {
				t.Fatalf("request=%+v", control.request)
			}
		})
	}
}

func TestFiniteRunAPIConflictReturnsActiveRunDeadline(t *testing.T) {
	control := newFakeRunControl()
	activeAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	endsAt := activeAt.Add(20 * time.Minute)
	control.start = evidence.RunStatus{State: evidence.RunRecording, RunID: "evr_active", Finite: true, DurationSeconds: 1200, RequestedAt: &activeAt, EndsAt: &endsAt}
	control.err = evidence.ErrRunActive
	server, _ := New(openStore(t), fakeDecoder{}, testToken, time.Minute, control)
	request := httptest.NewRequest(http.MethodPost, "/v1/evidence/runs", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"EVIDENCE_RUN_ACTIVE"`) || !strings.Contains(response.Body.String(), `"endsAt":`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func (c *fakeRunControl) Status() evidence.RunStatus { return c.status }

func (c *fakeRunControl) RunStatus(runID string) (evidence.RunStatus, error) {
	status, ok := c.runs[runID]
	if !ok {
		return evidence.RunStatus{}, evidence.ErrRunNotFound
	}
	return status, nil
}

func testConfig() evidence.Config {
	return evidence.Config{SchemaVersion: evidence.SchemaVersion, ModuleID: "test/evidence", Kind: "evidence-recorder", Runtime: evidence.RuntimeID, TargetExecutable: "Game.exe", Recording: evidence.RecordingConfig{Width: 1920, Height: 1080, FramesPerSecond: 1, SegmentSeconds: 2, Codec: "h264", Container: "mp4", Bitrate: 4_000_000}, FrameTap: evidence.FrameTapConfig{Name: `Local\WindowsAgent.Evidence.Test.v1`}, MaxRangeSeconds: 60}
}

type fakeFactory struct{}

func (fakeFactory) Open(path string, _ evidence.VideoFormat) (evidence.SegmentEncoder, error) {
	return fakeEncoder{path: path}, nil
}

type fakeEncoder struct{ path string }

func (fakeEncoder) WriteFrame(context.Context, uint64, []byte) error { return nil }
func (e fakeEncoder) Finalize(context.Context) error {
	return os.WriteFile(e.path, []byte("fake-mp4"), 0o600)
}

func openStore(t *testing.T) *evidence.Store {
	t.Helper()
	store, err := evidence.OpenStore(t.TempDir(), testConfig(), fakeFactory{})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestRangeRequiresAuthAndEnforcesBound(t *testing.T) {
	server, err := New(openStore(t), fakeDecoder{}, testToken, time.Minute, newFakeRunControl())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A02%3A00Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", response.Code)
	}
}

func TestActiveRecordingRangeReturnsConflict(t *testing.T) {
	store := openStore(t)
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	frame := &videocapture.Frame{Sequence: 1, ScheduledAt: at, ObservedAt: at, ForegroundExecutable: "Game.exe", Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: make([]byte, 1920*1080*4)}
	if _, err := store.Append(context.Background(), videocapture.Sample{ScheduledAt: at, Frame: frame}); err != nil {
		t.Fatal(err)
	}
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, newFakeRunControl())
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || response.Body.String() == "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthorizedEmptyRangeReturnsZip(t *testing.T) {
	server, err := New(openStore(t), fakeDecoder{}, testToken, time.Minute, newFakeRunControl())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestContactSheetRequiresStrictRequestAndReturnsJPEG(t *testing.T) {
	store := openStore(t)
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	frame := &videocapture.Frame{Sequence: 1, ScheduledAt: at, ObservedAt: at, ForegroundExecutable: "Game.exe", Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: make([]byte, 1920*1080*4)}
	if _, err := store.Append(context.Background(), videocapture.Sample{ScheduledAt: at, Frame: frame}); err != nil {
		t.Fatal(err)
	}
	frame.Sequence = 2
	frame.ScheduledAt = at.Add(time.Second)
	frame.ObservedAt = frame.ScheduledAt
	if _, err := store.Append(context.Background(), videocapture.Sample{ScheduledAt: frame.ScheduledAt, Frame: frame}); err != nil {
		t.Fatal(err)
	}
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, newFakeRunControl())

	request := httptest.NewRequest(http.MethodPost, "/v1/evidence/contact-sheet", strings.NewReader(`{"from":"2026-08-12T12:00:00Z","columns":1,"rows":1,"intervalSeconds":1}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("X-Evidence-Cells") != "1" || response.Body.Len() < 100 {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/evidence/contact-sheet", strings.NewReader(`{"from":"2026-08-12T12:00:00Z","columns":1,"columns":2,"rows":1,"intervalSeconds":1}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "strict JSON") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestContactSheetReturnsConflictForActiveSegment(t *testing.T) {
	store := openStore(t)
	at := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	frame := &videocapture.Frame{Sequence: 1, ScheduledAt: at, ObservedAt: at, ForegroundExecutable: "Game.exe", Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: make([]byte, 1920*1080*4)}
	_, _ = store.Append(context.Background(), videocapture.Sample{ScheduledAt: at, Frame: frame})
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, newFakeRunControl())
	request := httptest.NewRequest(http.MethodPost, "/v1/evidence/contact-sheet", strings.NewReader(`{"from":"2026-08-12T12:00:00Z","columns":1,"rows":1,"intervalSeconds":1}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "EVIDENCE_RANGE_NOT_COMMITTED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
