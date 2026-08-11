// Package eventhttp exposes the authenticated local append and replay surface
// for the independent Windows event-stream process.
package eventhttp

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const maxRequestBytes = eventstream.MaxEventBytes

type Server struct {
	store *eventstream.Store
	token string
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type replayResponse struct {
	Events       []eventstream.Event `json:"events"`
	NextCursor   uint64              `json:"nextCursor"`
	LastSequence uint64              `json:"lastSequence"`
}

type timeRangeResponse struct {
	Events       []eventstream.Event `json:"events"`
	NextCursor   uint64              `json:"nextCursor"`
	LastSequence uint64              `json:"lastSequence"`
	Complete     bool                `json:"complete"`
}

func New(store *eventstream.Store, token string) (*Server, error) {
	if store == nil {
		return nil, errors.New("event journal store is required")
	}
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("event API token must be canonical and at least 32 bytes")
	}
	return &Server{store: store, token: token}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "/v1/events":
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid local event API token is required")
			return
		}
		switch r.Method {
		case http.MethodPost:
			s.handleAppend(w, r)
		case http.MethodGet:
			s.handleReplay(w, r)
		default:
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	case "/v1/events/stream":
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid local event API token is required")
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleStream(w, r)
	case "/v1/events/range":
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid local event API token is required")
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleTimeRange(w, r)
	default:
		writeError(w, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) handleTimeRange(w http.ResponseWriter, r *http.Request) {
	if unknown := unknownQuery(r, "from", "to", "stream", "after", "limit"); unknown != "" {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", "unknown query parameter: "+unknown)
		return
	}
	from, err := parseUTCQuery(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", err.Error())
		return
	}
	to, err := parseUTCQuery(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", err.Error())
		return
	}
	streamValues := r.URL.Query()["stream"]
	if len(streamValues) != 1 || streamValues[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", "stream must appear exactly once and be non-empty")
		return
	}
	after, err := parseUintQuery(r, "after", false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", err.Error())
		return
	}
	limitValue, err := parseUintQuery(r, "limit", false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", err.Error())
		return
	}
	limit := eventstream.DefaultReplayLimit
	if _, specified := r.URL.Query()["limit"]; specified {
		if limitValue == 0 || limitValue > eventstream.MaxReplayLimit {
			writeError(w, http.StatusBadRequest, "invalid_time_range_request", fmt.Sprintf("limit must be between 1 and %d", eventstream.MaxReplayLimit))
			return
		}
		limit = int(limitValue)
	}
	result, err := s.store.ReadTimeRange(r.Context(), after, from, to, streamValues[0], limit)
	if errors.Is(err, eventstream.ErrCursorAhead) {
		writeError(w, http.StatusConflict, "event_cursor_ahead", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_time_range_request", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, timeRangeResponse{
		Events: result.Events, NextCursor: result.NextCursor, LastSequence: result.LastSequence, Complete: result.Complete,
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
	last, err := s.store.LastSequence()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "event_stream_failed", err.Error())
		return
	}
	if after > last {
		writeError(w, http.StatusConflict, "event_cursor_ahead", fmt.Sprintf("%v: cursor=%d lastSequence=%d", eventstream.ErrCursorAhead, after, last))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "event_stream_failed", "HTTP response streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	encoder := json.NewEncoder(w)
	cursor := after
	for {
		events, err := s.store.WaitAfter(r.Context(), cursor, eventstream.DefaultReplayLimit)
		if err != nil {
			return
		}
		for _, event := range events {
			if err := encoder.Encode(event); err != nil {
				return
			}
			cursor = event.Sequence
		}
		flusher.Flush()
	}
}

func (s *Server) handleAppend(w http.ResponseWriter, r *http.Request) {
	var request eventstream.AppendRequest
	if err := decodeStrictBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_event", err.Error())
		return
	}
	event, err := s.store.Append(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "event_append_failed", err.Error())
		return
	}
	w.Header().Set("Location", "/v1/events?after="+strconv.FormatUint(event.Sequence-1, 10)+"&limit=1")
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if unknown := unknownQuery(r, "after", "limit"); unknown != "" {
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
	if _, specified := r.URL.Query()["limit"]; specified {
		if limitValue == 0 {
			writeError(w, http.StatusBadRequest, "invalid_replay_request", "limit must be positive")
			return
		}
		if limitValue > eventstream.MaxReplayLimit {
			writeError(w, http.StatusBadRequest, "invalid_replay_request", fmt.Sprintf("limit must not exceed %d", eventstream.MaxReplayLimit))
			return
		}
		limit = int(limitValue)
	}
	events, err := s.store.ReadAfter(r.Context(), after, limit)
	if errors.Is(err, eventstream.ErrCursorAhead) {
		writeError(w, http.StatusConflict, "event_cursor_ahead", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "event_replay_failed", err.Error())
		return
	}
	last, err := s.store.LastSequence()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "event_replay_failed", err.Error())
		return
	}
	next := after
	if len(events) != 0 {
		next = events[len(events)-1].Sequence
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, replayResponse{Events: events, NextCursor: next, LastSequence: last})
}

func (s *Server) authorized(r *http.Request) bool {
	prefix := "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return len(provided) == len(s.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func decodeStrictBody(w http.ResponseWriter, r *http.Request, target any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Content-Type must equal application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(data) == 0 {
		return errors.New("request body is required")
	}
	if err := strictjson.Validate(data); err != nil {
		return fmt.Errorf("request body must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return fmt.Errorf("decode trailing request body: %w", err)
	}
	return nil
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

func parseUTCQuery(r *http.Request, name string) (time.Time, error) {
	values := r.URL.Query()[name]
	if len(values) != 1 || values[0] == "" {
		return time.Time{}, fmt.Errorf("%s must appear exactly once and be non-empty", name)
	}
	value, err := time.Parse(time.RFC3339Nano, values[0])
	if err != nil || value.Location() != time.UTC {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 UTC timestamp", name)
	}
	return value, nil
}

func unknownQuery(r *http.Request, allowed ...string) string {
	known := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		known[name] = struct{}{}
	}
	for name := range r.URL.Query() {
		if _, exists := known[name]; !exists {
			return name
		}
	}
	return ""
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}
