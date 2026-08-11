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
	server, err := New(openStore(t), fakeDecoder{}, testToken, time.Minute, func() Status { return Status{State: "recording"} })
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
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, func() Status { return Status{State: "recording"} })
	request := httptest.NewRequest(http.MethodGet, "/v1/evidence/range?from=2026-08-11T12%3A00%3A00Z&to=2026-08-11T12%3A00%3A01Z", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || response.Body.String() == "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthorizedEmptyRangeReturnsZip(t *testing.T) {
	server, err := New(openStore(t), fakeDecoder{}, testToken, time.Minute, func() Status { return Status{State: "recording"} })
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
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, func() Status { return Status{State: "recording"} })

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
	server, _ := New(store, fakeDecoder{}, testToken, time.Minute, func() Status { return Status{State: "recording"} })
	request := httptest.NewRequest(http.MethodPost, "/v1/evidence/contact-sheet", strings.NewReader(`{"from":"2026-08-12T12:00:00Z","columns":1,"rows":1,"intervalSeconds":1}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "EVIDENCE_RANGE_NOT_COMMITTED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
