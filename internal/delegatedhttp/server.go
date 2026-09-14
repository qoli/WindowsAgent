// Package delegatedhttp exposes the authenticated host-facing delegated-task API.
package delegatedhttp

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/qoli/WindowsAgent/internal/delegatedtask"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const maxRequestBytes = 1 << 20

type TaskService interface {
	Submit(context.Context, delegatedtask.SubmitRequest) (delegatedtask.Task, error)
	Get(string) (delegatedtask.Task, error)
	Send(context.Context, string, delegatedtask.MessageRequest) (delegatedtask.Task, error)
	Cancel(context.Context, string) (delegatedtask.Task, error)
	Events(context.Context, string, uint64, int) ([]eventstream.Event, uint64, uint64, error)
	WaitEvents(context.Context, string, uint64) ([]eventstream.Event, error)
}

type Server struct {
	tasks TaskService
	token string
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(tasks TaskService, token string) (*Server, error) {
	if tasks == nil {
		return nil, errors.New("delegated task service is required")
	}
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("delegated task API token must be canonical and at least 32 bytes")
	}
	return &Server{tasks: tasks, token: token}, nil
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) serveHTTP(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/healthz" {
		if request.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "runtime": "windows-agent-pi"})
		return
	}
	if !s.authorized(request) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid delegated task API token is required")
		return
	}
	path := strings.Trim(request.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "v1" && parts[1] == "delegated-tasks" {
		if request.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input delegatedtask.SubmitRequest
		if err := decodeBody(request, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
			return
		}
		task, err := s.tasks.Submit(request.Context(), input)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "task_submit_failed", err.Error())
			return
		}
		w.Header().Set("Location", "/v1/delegated-tasks/"+task.TaskID)
		writeJSON(w, http.StatusCreated, task)
		return
	}
	if len(parts) < 3 || len(parts) > 5 || parts[0] != "v1" || parts[1] != "delegated-tasks" || parts[2] == "" {
		writeError(w, http.StatusNotFound, "route_not_found", "route not found")
		return
	}
	taskID := parts[2]
	if len(parts) == 5 {
		if parts[3] != "events" || parts[4] != "stream" {
			writeError(w, http.StatusNotFound, "route_not_found", "route not found")
			return
		}
		if request.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleEventStream(w, request, taskID)
		return
	}
	if len(parts) == 3 {
		if request.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		task, err := s.tasks.Get(taskID)
		s.writeTask(w, task, err)
		return
	}
	switch parts[3] {
	case "events":
		if request.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.handleEvents(w, request, taskID)
	case "messages":
		if request.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var input delegatedtask.MessageRequest
		if err := decodeBody(request, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_message", err.Error())
			return
		}
		task, err := s.tasks.Send(request.Context(), taskID, input)
		s.writeTask(w, task, err)
	case "cancel":
		if request.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		payload, err := io.ReadAll(io.LimitReader(request.Body, 1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cancel", err.Error())
			return
		}
		if len(payload) > 0 {
			writeError(w, http.StatusBadRequest, "invalid_cancel", "cancel request must not contain a body")
			return
		}
		task, err := s.tasks.Cancel(request.Context(), taskID)
		s.writeTask(w, task, err)
	default:
		writeError(w, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) handleEventStream(w http.ResponseWriter, request *http.Request, taskID string) {
	if unknownQuery(request, "after") != "" {
		writeError(w, http.StatusBadRequest, "invalid_stream_request", "unknown query parameter")
		return
	}
	after, err := parseUint(request, "after", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_stream_request", err.Error())
		return
	}
	initial, next, _, err := s.tasks.Events(request.Context(), taskID, after, eventstream.DefaultReplayLimit)
	if errors.Is(err, delegatedtask.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "task_not_found", err.Error())
		return
	} else if errors.Is(err, eventstream.ErrCursorAhead) {
		writeError(w, http.StatusConflict, "event_cursor_ahead", err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "event_replay_failed", err.Error())
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
	for _, event := range initial {
		if err := encoder.Encode(event); err != nil {
			return
		}
		if terminalTaskEvent(event.Type) {
			flusher.Flush()
			return
		}
	}
	after = next
	flusher.Flush()
	for {
		events, err := s.tasks.WaitEvents(request.Context(), taskID, after)
		if err != nil {
			return
		}
		for _, event := range events {
			if err := encoder.Encode(event); err != nil {
				return
			}
			after = event.Sequence
			if terminalTaskEvent(event.Type) {
				flusher.Flush()
				return
			}
		}
		flusher.Flush()
	}
}

func terminalTaskEvent(eventType string) bool {
	return eventType == "task.completed" || eventType == "task.failed" || eventType == "task.cancelled"
}

func (s *Server) handleEvents(w http.ResponseWriter, request *http.Request, taskID string) {
	if unknownQuery(request, "after", "limit") != "" {
		writeError(w, http.StatusBadRequest, "invalid_events_request", "unknown query parameter")
		return
	}
	after, err := parseUint(request, "after", true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_events_request", err.Error())
		return
	}
	limit := eventstream.DefaultReplayLimit
	if _, present := request.URL.Query()["limit"]; present {
		value, err := parseUint(request, "limit", true)
		if err != nil || value == 0 || value > eventstream.MaxReplayLimit {
			writeError(w, http.StatusBadRequest, "invalid_events_request", fmt.Sprintf("limit must be between 1 and %d", eventstream.MaxReplayLimit))
			return
		}
		limit = int(value)
	}
	events, next, last, err := s.tasks.Events(request.Context(), taskID, after, limit)
	if errors.Is(err, delegatedtask.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "task_not_found", err.Error())
		return
	}
	if errors.Is(err, eventstream.ErrCursorAhead) {
		writeError(w, http.StatusConflict, "event_cursor_ahead", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "event_replay_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "nextCursor": next, "lastSequence": last})
}

func (s *Server) writeTask(w http.ResponseWriter, task delegatedtask.Task, err error) {
	if errors.Is(err, delegatedtask.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "task_not_found", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "task_operation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) authorized(request *http.Request) bool {
	want := "Bearer " + s.token
	got := request.Header.Get("Authorization")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func decodeBody(request *http.Request, target any) error {
	payload, err := io.ReadAll(io.LimitReader(request.Body, maxRequestBytes+1))
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maxRequestBytes {
		return errors.New("request body is required and must not exceed 1 MiB")
	}
	if err := strictjson.Validate(payload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func parseUint(request *http.Request, name string, required bool) (uint64, error) {
	values, present := request.URL.Query()[name]
	if !present {
		if required {
			return 0, fmt.Errorf("%s must appear exactly once", name)
		}
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, fmt.Errorf("%s must appear exactly once", name)
	}
	value, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer", name)
	}
	return value, nil
}

func unknownQuery(request *http.Request, allowed ...string) string {
	set := map[string]bool{}
	for _, name := range allowed {
		set[name] = true
	}
	for name := range request.URL.Query() {
		if !set[name] {
			return name
		}
	}
	return ""
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
