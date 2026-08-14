package eventweb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeSource struct {
	events     []eventstream.Event
	next       uint64
	last       uint64
	replayErr  error
	streamErr  error
	lastStream string
}

func (f *fakeSource) Replay(_ context.Context, _ uint64, _ int) ([]eventstream.Event, uint64, uint64, error) {
	return f.events, f.next, f.last, f.replayErr
}

func (f *fakeSource) ReplayStream(_ context.Context, _ uint64, _ int, stream string) ([]eventstream.Event, uint64, uint64, error) {
	f.lastStream = stream
	return f.events, f.next, f.last, f.replayErr
}

func (f *fakeSource) Stream(_ context.Context, after uint64, visit func(eventstream.Event) error) error {
	for _, event := range f.events {
		if event.Sequence > after {
			if err := visit(event); err != nil {
				return err
			}
		}
	}
	return f.streamErr
}

type fakeIndicators struct {
	capture, recording bool
	captureErr         error
	recordingErr       error
}

func (f fakeIndicators) CaptureActive() (bool, error)   { return f.capture, f.captureErr }
func (f fakeIndicators) RecordingActive() (bool, error) { return f.recording, f.recordingErr }

func TestServerServesEmbeddedUIAndRequiresAPIToken(t *testing.T) {
	server := newTestServer(t, &fakeSource{}, &Projection{}, fakeIndicators{})

	ui := httptest.NewRecorder()
	server.Handler().ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/", nil))
	if ui.Code != http.StatusOK || !bytes.Contains(ui.Body.Bytes(), []byte("WindowsAgent Live")) {
		t.Fatalf("unexpected UI response: status=%d body=%s", ui.Code, ui.Body.String())
	}
	if ui.Header().Get("Content-Security-Policy") == "" || ui.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("UI response is missing browser security headers")
	}

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/osd", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("unexpected unauthorized response: status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
}

func TestEmbeddedUISeparatesTabsWithoutSplittingGlobalLiveStream(t *testing.T) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, marker := range []string{
		`role="tablist"`,
		`aria-selected="true" aria-controls="events" data-stream="action.runs"`,
		`aria-selected="false" aria-controls="events" data-stream="visual-log"`,
		`const state = { token: sessionStorage.getItem('windowsAgentWebToken') || '', cursor:'0', activeStream:streams.actionRuns`,
		`x.event.stream===state.activeStream`,
		`state.events.length>eventBufferLimit`,
		`/api/v1/events/stream?after=`,
	} {
		if !strings.Contains(ui, marker) {
			t.Fatalf("embedded UI is missing streaming-log contract marker %q", marker)
		}
	}
	if strings.Count(ui, "/api/v1/events/stream?after=") != 1 {
		t.Fatal("embedded UI must maintain one unfiltered live stream")
	}
	if strings.Contains(ui, "state.cursor=maxCursor(state.cursor,data.cursor)") {
		t.Fatal("OSD polling must not advance the global event cursor")
	}
}

func TestEmbeddedUIBootstrapsRecentTailAndBatchesRendering(t *testing.T) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, marker := range []string{
		`const initialReplayLimit=100, eventBufferLimit=500, visibleEventLimit=250, renderDelayMs=100, parseYieldEvery=100;`,
		`/api/v1/events?after=0&limit=1`,
		`const lastSequence=BigInt(probe.lastSequence)`,
		`const tailStart=lastSequence>BigInt(initialReplayLimit)?String(lastSequence-BigInt(initialReplayLimit)):'0'`,
		`/api/v1/events?after='+tailStart+'&limit='+initialReplayLimit`,
		`state.cursor=String(data.nextCursor)`,
		`state.renderTimer=setTimeout(`,
		`processedSinceYield>=parseYieldEvery`,
		`await new Promise(resolve=>setTimeout(resolve,0))`,
	} {
		if !strings.Contains(ui, marker) {
			t.Fatalf("embedded UI is missing bounded streaming marker %q", marker)
		}
	}
	addStart := strings.Index(ui, "function addEnvelope(")
	addEnd := strings.Index(ui, "function renderEvents(")
	if addStart < 0 || addEnd <= addStart {
		t.Fatal("embedded UI addEnvelope function is unavailable")
	}
	addEnvelope := ui[addStart:addEnd]
	if !strings.Contains(addEnvelope, "scheduleEventsRender()") || strings.Contains(addEnvelope, "renderEvents()") {
		t.Fatal("addEnvelope must schedule one bounded render instead of rebuilding the DOM per envelope")
	}
}

func TestEmbeddedUIUsesNonBlockingTokenPanel(t *testing.T) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, marker := range []string{
		`<div id="token-panel" class="token-panel" hidden>`,
		`<form id="token-form">`,
		`<input id="web-token" type="password" autocomplete="off" required>`,
		`function showTokenPanel(status='token required')`,
		`sessionStorage.removeItem('windowsAgentWebToken')`,
		`$('token-form').onsubmit=submitToken`,
	} {
		if !strings.Contains(ui, marker) {
			t.Fatalf("embedded UI is missing non-blocking token marker %q", marker)
		}
	}
	if strings.Contains(ui, "prompt(") || strings.Contains(ui, "alert(") || strings.Contains(ui, "confirm(") {
		t.Fatal("embedded UI must not use blocking browser dialogs")
	}
}

func TestEmbeddedUIKeepsOSDIndicatorsFixedAndGreyWhenInactive(t *testing.T) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, marker := range []string{
		`.row { display:flex; align-items:center; gap:9px; margin-bottom:12px }`,
		`.dot { width:10px; height:10px; border-radius:50%; background:#485566; flex:0 0 auto }`,
		`.indicator { width:100% }`,
		`const captureIndicator='<div class="row indicator"><span class="dot'+(data.captureActive?' capture':'')+'"></span><span>'+(data.captureActive?'Capture accepted':'Capture idle')+'</span></div>';`,
		`const recordingIndicator='<div class="row indicator"><span class="dot'+(data.recordingActive?' recording':'')+'"></span><span>'+(data.recordingActive?'Evidence recording':'Evidence idle')+'</span></div>';`,
		`$('osd').innerHTML=captureIndicator+recordingIndicator+'<div class="row">'+dot`,
	} {
		if !strings.Contains(ui, marker) {
			t.Fatalf("embedded UI is missing fixed OSD indicator contract marker %q", marker)
		}
	}
	if strings.Count(ui, `class="row indicator"`) != 2 {
		t.Fatalf("OSD renderer must define exactly two indicator rows, got %d", strings.Count(ui, `class="row indicator"`))
	}
	if strings.Contains(ui, "if(data.captureActive) indicators.push") || strings.Contains(ui, "if(data.recordingActive) indicators.push") {
		t.Fatal("OSD indicators must remain rendered when inactive")
	}
	if strings.Index(ui, "captureIndicator") > strings.Index(ui, "recordingIndicator") ||
		strings.Index(ui, "recordingIndicator") > strings.Index(ui, "$('osd').innerHTML=captureIndicator+recordingIndicator") {
		t.Fatal("OSD indicator rows must precede the Action row")
	}
}

func TestServerProjectsActionOSDAndSessionIndicators(t *testing.T) {
	projection := &Projection{}
	start := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	if err := projection.Apply(actionEvent(41, "action.started", start,
		`{"state":"RUNNING","actionId":"game/leave","lifecycle":"linear","interruptible":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := projection.Apply(actionEvent(42, "action.activity", start.Add(time.Second),
		`{"message":"Leaving station","level":"info"}`)); err != nil {
		t.Fatal(err)
	}
	projection.SetConnection(true, 42)
	server := newTestServer(t, &fakeSource{}, projection, fakeIndicators{capture: true, recording: true})
	server.now = func() time.Time { return start.Add(2 * time.Second) }

	response := httptest.NewRecorder()
	request := authorizedRequest(http.MethodGet, "/api/v1/osd")
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected OSD response: status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Cursor          string `json:"cursor"`
		StreamConnected bool   `json:"streamConnected"`
		CaptureActive   bool   `json:"captureActive"`
		RecordingActive bool   `json:"recordingActive"`
		Action          struct {
			Visible    bool   `json:"visible"`
			ActionID   string `json:"actionId"`
			Activities []struct {
				Message string `json:"message"`
			} `json:"activities"`
		} `json:"action"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Cursor != "42" || !body.StreamConnected || !body.CaptureActive || !body.RecordingActive ||
		!body.Action.Visible || body.Action.ActionID != "game/leave" || len(body.Action.Activities) != 1 ||
		body.Action.Activities[0].Message != "Leaving station" {
		t.Fatalf("unexpected OSD projection: %+v", body)
	}
}

func TestServerUsesStringCursorEnvelopeForReplayAndLiveStream(t *testing.T) {
	sequence := uint64(9007199254740993)
	event := actionEvent(sequence, "action.activity", time.Now().UTC(), `{"message":"Exact cursor","level":"info"}`)
	source := &fakeSource{events: []eventstream.Event{event}, next: sequence, last: sequence}
	server := newTestServer(t, source, &Projection{}, fakeIndicators{})

	replay := httptest.NewRecorder()
	server.Handler().ServeHTTP(replay, authorizedRequest(http.MethodGet, "/api/v1/events?after=0&limit=1&stream=action.runs"))
	if replay.Code != http.StatusOK || source.lastStream != "action.runs" {
		t.Fatalf("unexpected replay response: status=%d body=%s stream=%q", replay.Code, replay.Body.String(), source.lastStream)
	}
	quoted := `"` + strconv.FormatUint(sequence, 10) + `"`
	if !strings.Contains(replay.Body.String(), `"cursor":`+quoted) ||
		!strings.Contains(replay.Body.String(), `"nextCursor":`+quoted) {
		t.Fatalf("replay cursor was not preserved as a JSON string: %s", replay.Body.String())
	}

	live := httptest.NewRecorder()
	server.Handler().ServeHTTP(live, authorizedRequest(http.MethodGet, "/api/v1/events/stream?after=0"))
	if live.Code != http.StatusOK || live.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("unexpected live response: status=%d content-type=%q", live.Code, live.Header().Get("Content-Type"))
	}
	line, err := bufio.NewReader(live.Body).ReadString('\n')
	if err != nil || !strings.Contains(line, `"cursor":`+quoted) {
		t.Fatalf("unexpected live envelope: line=%s err=%v", line, err)
	}
}

func TestServerReportsDegradedProjectionAndExplicitUpstreamFailures(t *testing.T) {
	source := &fakeSource{replayErr: errors.New("upstream unavailable")}
	server := newTestServer(t, source, &Projection{}, fakeIndicators{})

	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusServiceUnavailable || !strings.Contains(health.Body.String(), `"status":"degraded"`) {
		t.Fatalf("unexpected degraded health: status=%d body=%s", health.Code, health.Body.String())
	}

	replay := httptest.NewRecorder()
	server.Handler().ServeHTTP(replay, authorizedRequest(http.MethodGet, "/api/v1/events?after=0"))
	if replay.Code != http.StatusBadGateway || !strings.Contains(replay.Body.String(), "event_replay_failed") ||
		strings.Contains(replay.Body.String(), "upstream unavailable") {
		t.Fatalf("unexpected upstream failure response: status=%d body=%s", replay.Code, replay.Body.String())
	}

	indicatorServer := newTestServer(t, &fakeSource{}, &Projection{}, fakeIndicators{captureErr: errors.New("private detail")})
	indicator := httptest.NewRecorder()
	indicatorServer.Handler().ServeHTTP(indicator, authorizedRequest(http.MethodGet, "/api/v1/osd"))
	if indicator.Code != http.StatusInternalServerError || !strings.Contains(indicator.Body.String(), "capture_indicator_failed") ||
		strings.Contains(indicator.Body.String(), "private detail") {
		t.Fatalf("unexpected indicator failure response: status=%d body=%s", indicator.Code, indicator.Body.String())
	}
}

func TestServerRejectsUnknownAndNonCanonicalQueries(t *testing.T) {
	server := newTestServer(t, &fakeSource{}, &Projection{}, fakeIndicators{})
	for _, target := range []string{
		"/api/v1/events", "/api/v1/events?after=0&after=1", "/api/v1/events?after=0&unknown=1",
		"/api/v1/events/stream?after=-1", "/api/v1/osd?after=0",
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, authorizedRequest(http.MethodGet, target))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("target %s returned HTTP %d: %s", target, response.Code, response.Body.String())
		}
	}
}

func TestNewRejectsMissingDependenciesAndWeakToken(t *testing.T) {
	if _, err := New(nil, &Projection{}, fakeIndicators{}, testToken); err == nil {
		t.Fatal("missing source was accepted")
	}
	if _, err := New(&fakeSource{}, &Projection{}, fakeIndicators{}, "short"); err == nil {
		t.Fatal("weak token was accepted")
	}
}

func TestValidateListenAddressAllowsOnlyExplicitPrivateIPv4(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8790", "10.1.2.3:8790", "172.16.0.1:8790", "192.168.10.20:8790"} {
		if err := ValidateListenAddress(address); err != nil {
			t.Fatalf("valid address %q was rejected: %v", address, err)
		}
	}
	for _, address := range []string{"localhost:8790", "0.0.0.0:8790", "8.8.8.8:8790", "[::1]:8790", "192.168.10.20", "192.168.10.20:99999"} {
		if err := ValidateListenAddress(address); err == nil {
			t.Fatalf("invalid address %q was accepted", address)
		}
	}
}

func newTestServer(t *testing.T, source EventSource, projection *Projection, indicators Indicators) *Server {
	t.Helper()
	server, err := New(source, projection, indicators, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func authorizedRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	return request
}

func actionEvent(sequence uint64, kind string, observedAt time.Time, payload string) eventstream.Event {
	return eventstream.Event{
		SchemaVersion: 1, Sequence: sequence, EventID: "evt_test", SessionID: "session_test",
		Stream: "action.runs", Type: kind, ObservedAt: observedAt, CommittedAt: observedAt,
		Source:     eventstream.Source{ModuleID: "game/leave", InstanceID: "instance_test", Runtime: "test"},
		Foreground: eventstream.Foreground{ExecutableName: "Game.exe", Revision: 1}, CorrelationID: "act_test",
		Payload: json.RawMessage(payload),
	}
}
