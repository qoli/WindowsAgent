package visualloghttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/visuallog"
)

const controlToken = "0123456789abcdef0123456789abcdef"

type frameFixture struct{}

func (frameFixture) Latest(context.Context, time.Time) (visuallog.Frame, error) {
	now := time.Now().UTC()
	return visuallog.Frame{
		ScheduledAt: now.Truncate(time.Second), CaptureID: "cap_test", ObservedAt: now, ContentType: "image/jpeg", Content: []byte("jpeg"),
		ForegroundRevision: 1, ForegroundExecutable: "EliteDangerous64.exe",
	}, nil
}

type describerFixture struct{}

func (describerFixture) Describe(context.Context, visuallog.Frame) (visuallog.Description, error) {
	return visuallog.Description{
		Text: "Vast illuminated station interior surrounds large curved industrial docking structures.", ModelID: "gemma-4-e4b-it-8bit",
	}, nil
}

type eventFixture struct{ sequence uint64 }

func (f *eventFixture) Append(_ context.Context, request eventstream.AppendRequest) (eventstream.Event, error) {
	f.sequence++
	return eventstream.Event{Sequence: f.sequence, SessionID: request.SessionID, ObservedAt: request.ObservedAt, Type: request.Type}, nil
}

func TestAuthenticatedControlStartsAndStopsIndependentRun(t *testing.T) {
	controller := testController(t)
	defer controller.Close()
	server, err := New(controller, controlToken)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/visual-log/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	start := httptest.NewRequest(http.MethodPost, "/v1/visual-log/runs", bytes.NewReader([]byte(`{}`)))
	start.Header.Set("Content-Type", "application/json")
	start.Header.Set("Authorization", "Bearer "+controlToken)
	started := httptest.NewRecorder()
	server.Handler().ServeHTTP(started, start)
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	stop := httptest.NewRequest(http.MethodDelete, "/v1/visual-log/runs/current", nil)
	stop.Header.Set("Authorization", "Bearer "+controlToken)
	stopped := httptest.NewRecorder()
	server.Handler().ServeHTTP(stopped, stop)
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", stopped.Code, stopped.Body.String())
	}
}

func testController(t *testing.T) *visuallog.Controller {
	t.Helper()
	config := visuallog.Config{
		SchemaVersion: 2, ModuleID: "elite-dangerous/visual-log", Kind: "visual-log", Runtime: visuallog.RuntimeID,
		TargetExecutable: "EliteDangerous64.exe", IntervalMS: 2000, WarmupCalls: 1, Evidence: visuallog.EvidenceConfig{FrameTapName: `Local\WindowsAgent.Evidence.EliteDangerous.v1`, MaxFrameAgeMS: 3000, WarmupFrameTimeoutMS: 5000},
		Prompt: "Describe the directly visible physical scene in this single Elite Dangerous screenshot using 8-16 words.",
		Model:  visuallog.ModelConfig{ID: "gemma-4-e4b-it-8bit", MaxTokens: 64, Temperature: 1, TopP: 0.95, TopK: 64},
		Output: visuallog.OutputConfig{
			Stream: "visual-log", ObservationType: "visual-log.observation", FailureType: "visual-log.failure",
			DescriptionMinWords: 8, DescriptionMaxWords: 16,
		},
	}
	runner := visuallog.Runner{
		Config: config, Frames: frameFixture{}, Describer: describerFixture{}, Events: &eventFixture{},
		SessionID: "bootstrap_session", InstanceID: "instance_1",
	}
	controller, err := visuallog.NewController(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}
