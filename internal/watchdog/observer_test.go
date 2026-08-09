package watchdog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeProcessInspector struct {
	processes []ProcessInfo
	err       error
	path      string
}

func (f *fakeProcessInspector) FindByExecutablePath(_ context.Context, path string) ([]ProcessInfo, error) {
	f.path = path
	return f.processes, f.err
}

func TestTargetObserverRequiresEveryConfiguredProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	interactive := true
	inspector := &fakeProcessInspector{processes: []ProcessInfo{{PID: 42, SessionID: 1}}}
	observer, err := NewTargetObserver(server.Client(), inspector)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{ID: "target", Probes: []ProbeConfig{
		{Type: "process", ExecutablePath: `C:\Agent\target.exe`, RequireInteractiveSession: &interactive},
		{Type: "http-json", URL: server.URL, TimeoutMS: 1000, ExpectedStatusCode: 200, ExpectedJSONStatus: "ok"},
	}}
	observation, err := observer.Observe(context.Background(), target)
	if err != nil || !observation.Healthy || inspector.path != `C:\Agent\target.exe` {
		t.Fatalf("observation=%+v path=%q error=%v", observation, inspector.path, err)
	}

	inspector.processes = nil
	observation, err = observer.Observe(context.Background(), target)
	if err != nil || observation.Healthy || !strings.Contains(observation.Detail, "absent") {
		t.Fatalf("observation=%+v error=%v", observation, err)
	}
}

func TestTargetObserverDoesNotAcceptLooseHealthPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","extra":true}`))
	}))
	defer server.Close()
	observer, err := NewTargetObserver(server.Client(), &fakeProcessInspector{})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observer.observeHTTP(context.Background(), ProbeConfig{
		Type: "http-json", URL: server.URL, TimeoutMS: 1000, ExpectedStatusCode: 200, ExpectedJSONStatus: "ok",
	})
	if err != nil || observation.Healthy || !strings.Contains(observation.Detail, "strict status contract") {
		t.Fatalf("observation=%+v error=%v", observation, err)
	}
}
