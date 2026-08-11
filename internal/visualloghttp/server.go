// Package visualloghttp exposes authenticated control of one independent visual-log process.
package visualloghttp

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/qoli/WindowsAgent/internal/visuallog"
)

const maxControlRequestBytes = 4096

type Server struct {
	controller *visuallog.Controller
	token      string
}

func New(controller *visuallog.Controller, token string) (*Server, error) {
	if controller == nil {
		return nil, errors.New("visual log controller is required")
	}
	if len(token) < 32 || strings.TrimSpace(token) != token {
		return nil, errors.New("visual log control token must be canonical and at least 32 bytes")
	}
	return &Server{controller: controller, token: token}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized", "a valid visual log control token is required")
		return
	}
	switch r.URL.Path {
	case "/v1/visual-log/status":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, s.controller.Status())
	case "/v1/visual-log/runs":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if err := decodeEmptyRequest(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_run_request", err.Error())
			return
		}
		status, err := s.controller.Start()
		if errors.Is(err, visuallog.ErrRunActive) {
			writeError(w, http.StatusConflict, "visual_log_already_active", err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "visual_log_start_failed", err.Error())
			return
		}
		w.Header().Set("Location", "/v1/visual-log/status")
		writeJSON(w, http.StatusCreated, status)
	case "/v1/visual-log/runs/current":
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodDelete)
			return
		}
		status, err := s.controller.Stop()
		if errors.Is(err, visuallog.ErrRunInactive) {
			writeError(w, http.StatusConflict, "visual_log_not_active", err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "visual_log_stop_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		writeError(w, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return len(provided) == len(s.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func decodeEmptyRequest(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Content-Type must equal application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxControlRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request struct{}
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	return nil
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
