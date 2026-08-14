// Package eventweb exposes a browser-facing, read-only projection of the
// authenticated loopback event journal and Action OSD state.
package eventweb

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionosd"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

//go:embed assets/index.html
var assets embed.FS

type EventSource interface {
	Replay(context.Context, uint64, int) ([]eventstream.Event, uint64, uint64, error)
	ReplayStream(context.Context, uint64, int, string) ([]eventstream.Event, uint64, uint64, error)
	Stream(context.Context, uint64, func(eventstream.Event) error) error
}

type Indicators interface {
	CaptureActive() (bool, error)
	RecordingActive() (bool, error)
}

type Projection struct {
	model actionosd.Model

	mu        sync.RWMutex
	cursor    uint64
	connected bool
}

type Server struct {
	source     EventSource
	projection *Projection
	indicators Indicators
	token      string
	now        func() time.Time
}

type webEvent struct {
	Cursor string            `json:"cursor"`
	Event  eventstream.Event `json:"event"`
}

type replayResponse struct {
	Events       []webEvent `json:"events"`
	NextCursor   string     `json:"nextCursor"`
	LastSequence string     `json:"lastSequence"`
}

type osdResponse struct {
	GeneratedAt     time.Time         `json:"generatedAt"`
	Cursor          string            `json:"cursor"`
	StreamConnected bool              `json:"streamConnected"`
	CaptureActive   bool              `json:"captureActive"`
	RecordingActive bool              `json:"recordingActive"`
	Action          actionOSDResponse `json:"action"`
}

type actionOSDResponse struct {
	Visible      bool               `json:"visible"`
	Status       string             `json:"status,omitempty"`
	InvocationID string             `json:"invocationId,omitempty"`
	ActionID     string             `json:"actionId,omitempty"`
	StartedAt    *time.Time         `json:"startedAt,omitempty"`
	TerminalAt   *time.Time         `json:"terminalAt,omitempty"`
	Activities   []activityResponse `json:"activities"`
}

type activityResponse struct {
	ObservedAt time.Time `json:"observedAt"`
	Message    string    `json:"message"`
	Level      string    `json:"level"`
}

func New(source EventSource, projection *Projection, indicators Indicators, token string) (*Server, error) {
	if source == nil || projection == nil || indicators == nil {
		return nil, errors.New("event source, OSD projection, and indicators are required")
	}
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("web token must be canonical and at least 32 bytes")
	}
	return &Server{source: source, projection: projection, indicators: indicators, token: token, now: time.Now}, nil
}

func ValidateListenAddress(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return fmt.Errorf("--listen must be an explicit IPv4 host and port: %q", listen)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("--listen port must be between 1 and 65535: %q", listen)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return fmt.Errorf("--listen must use an explicit loopback or private LAN IPv4 address: %q", listen)
	}
	return nil
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (p *Projection) Apply(event eventstream.Event) error {
	if err := p.model.Apply(event); err != nil {
		return err
	}
	p.mu.Lock()
	if event.Sequence > p.cursor {
		p.cursor = event.Sequence
	}
	p.mu.Unlock()
	return nil
}

func (p *Projection) SetConnection(connected bool, cursor uint64) {
	p.mu.Lock()
	p.connected = connected
	if cursor > p.cursor {
		p.cursor = cursor
	}
	p.mu.Unlock()
}

func (p *Projection) state(now time.Time) (actionosd.Snapshot, uint64, bool) {
	p.mu.RLock()
	cursor, connected := p.cursor, p.connected
	p.mu.RUnlock()
	return p.model.Snapshot(now), cursor, connected
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	switch r.URL.Path {
	case "/":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "invalid_ui_request", "query parameters are forbidden")
			return
		}
		data, err := assets.ReadFile("assets/index.html")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ui_unavailable", "embedded UI is unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case "/healthz":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "invalid_health_request", "query parameters are forbidden")
			return
		}
		_, cursor, connected := s.projection.state(s.now().UTC())
		status, code := "ok", http.StatusOK
		if !connected {
			status, code = "degraded", http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]string{"status": status, "cursor": strconv.FormatUint(cursor, 10)})
	case "/api/v1/osd":
		if !s.authorized(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleOSD(w, r)
	case "/api/v1/events":
		if !s.authorized(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleReplay(w, r)
	case "/api/v1/events/stream":
		if !s.authorized(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleStream(w, r)
	default:
		writeError(w, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) handleOSD(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_osd_request", "query parameters are forbidden")
		return
	}
	capture, err := s.indicators.CaptureActive()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "capture_indicator_failed", "capture indicator is unavailable")
		return
	}
	recording, err := s.indicators.RecordingActive()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "recording_indicator_failed", "recording indicator is unavailable")
		return
	}
	now := s.now().UTC()
	snapshot, cursor, connected := s.projection.state(now)
	response := osdResponse{
		GeneratedAt: now, Cursor: strconv.FormatUint(cursor, 10), StreamConnected: connected,
		CaptureActive: capture, RecordingActive: recording,
		Action: actionOSDResponse{
			Visible: snapshot.Visible, Status: snapshot.Status, InvocationID: snapshot.InvocationID,
			ActionID: snapshot.ActionID, Activities: make([]activityResponse, 0, len(snapshot.Activities)),
		},
	}
	if !snapshot.StartedAt.IsZero() {
		started := snapshot.StartedAt
		response.Action.StartedAt = &started
	}
	if !snapshot.TerminalAt.IsZero() {
		terminal := snapshot.TerminalAt
		response.Action.TerminalAt = &terminal
	}
	for _, activity := range snapshot.Activities {
		response.Action.Activities = append(response.Action.Activities, activityResponse{
			ObservedAt: activity.ObservedAt, Message: activity.Message, Level: activity.Level,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if unknown := unknownQuery(r, "after", "limit", "stream"); unknown != "" {
		writeError(w, http.StatusBadRequest, "invalid_replay_request", "unknown query parameter: "+unknown)
		return
	}
	after, err := parseUintQuery(r, "after", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_replay_request", err.Error())
		return
	}
	limitValue, err := parseUintQuery(r, "limit", false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_replay_request", err.Error())
		return
	}
	limit := eventstream.DefaultReplayLimit
	if _, ok := r.URL.Query()["limit"]; ok {
		if limitValue == 0 || limitValue > eventstream.MaxReplayLimit {
			writeError(w, http.StatusBadRequest, "invalid_replay_request", fmt.Sprintf("limit must be between 1 and %d", eventstream.MaxReplayLimit))
			return
		}
		limit = int(limitValue)
	}
	stream := ""
	if values, ok := r.URL.Query()["stream"]; ok {
		if len(values) != 1 || values[0] == "" {
			writeError(w, http.StatusBadRequest, "invalid_replay_request", "stream must appear exactly once and be non-empty")
			return
		}
		stream = values[0]
		if err := eventstream.ValidateStreamName(stream); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_replay_request", err.Error())
			return
		}
	}
	var events []eventstream.Event
	var next, last uint64
	if stream == "" {
		events, next, last, err = s.source.Replay(r.Context(), after, limit)
	} else {
		events, next, last, err = s.source.ReplayStream(r.Context(), after, limit, stream)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "event_replay_failed", "upstream event replay failed")
		return
	}
	writeJSON(w, http.StatusOK, replayResponse{
		Events: wrapEvents(events), NextCursor: strconv.FormatUint(next, 10), LastSequence: strconv.FormatUint(last, 10),
	})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if unknown := unknownQuery(r, "after"); unknown != "" {
		writeError(w, http.StatusBadRequest, "invalid_stream_request", "unknown query parameter: "+unknown)
		return
	}
	after, err := parseUintQuery(r, "after", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_stream_request", err.Error())
		return
	}
	if _, _, _, err := s.source.Replay(r.Context(), after, 1); err != nil {
		writeError(w, http.StatusBadGateway, "event_stream_failed", "upstream event stream is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "event_stream_failed", "HTTP response streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	encoder := json.NewEncoder(w)
	_ = s.source.Stream(r.Context(), after, func(event eventstream.Event) error {
		if err := encoder.Encode(webEvent{Cursor: strconv.FormatUint(event.Sequence, 10), Event: event}); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	provided := strings.TrimPrefix(header, prefix)
	if !strings.HasPrefix(header, prefix) || len(provided) != len(s.token) ||
		subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid web bearer token is required")
		return false
	}
	return true
}

func wrapEvents(events []eventstream.Event) []webEvent {
	result := make([]webEvent, 0, len(events))
	for _, event := range events {
		result = append(result, webEvent{Cursor: strconv.FormatUint(event.Sequence, 10), Event: event})
	}
	return result
}

func parseUintQuery(r *http.Request, name string, required bool) (uint64, error) {
	values, exists := r.URL.Query()[name]
	if !exists {
		if required {
			return 0, fmt.Errorf("%s is required", name)
		}
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, fmt.Errorf("%s must appear exactly once and be non-empty", name)
	}
	value, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer", name)
	}
	return value, nil
}

func unknownQuery(r *http.Request, allowed ...string) string {
	known := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		known[name] = struct{}{}
	}
	for name := range r.URL.Query() {
		if _, ok := known[name]; !ok {
			return name
		}
	}
	return ""
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
