package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/qoli/WindowsAgent/internal/artifact"
	"github.com/qoli/WindowsAgent/internal/capture"
)

const maxRequestBody = 4 << 10

type Server struct {
	capturer capture.Capturer
	store    *artifact.Store
	timeout  time.Duration
	version  string
	logger   *slog.Logger
	gate     chan struct{}
	sequence atomic.Uint64
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type captureRequest struct {
	IncludeCursor *bool `json:"include_cursor"`
}

type statusResponse struct {
	Service       string             `json:"service"`
	Version       string             `json:"version"`
	Capture       capture.Status     `json:"capture"`
	ArtifactRoot  string             `json:"artifact_root"`
	ArtifactCount int                `json:"artifact_count"`
	Latest        *artifact.Metadata `json:"latest,omitempty"`
}

func New(capturer capture.Capturer, store *artifact.Store, timeout time.Duration, version string, logger *slog.Logger) (*Server, error) {
	if capturer == nil {
		return nil, errors.New("capturer is required")
	}
	if store == nil {
		return nil, errors.New("artifact store is required")
	}
	if timeout <= 0 {
		return nil, errors.New("capture timeout must be positive")
	}
	if version == "" {
		return nil, errors.New("service version is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Server{
		capturer: capturer,
		store:    store,
		timeout:  timeout,
		version:  version,
		logger:   logger,
		gate:     make(chan struct{}, 1),
	}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := strconv.FormatUint(s.sequence.Add(1), 10)
	w.Header().Set("X-Request-ID", requestID)
	started := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		s.logger.InfoContext(r.Context(), "http_request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	}()

	switch {
	case r.URL.Path == "/healthz":
		s.requireMethod(recorder, r, requestID, http.MethodGet, s.handleHealth)
	case r.URL.Path == "/v1/status":
		s.requireMethod(recorder, r, requestID, http.MethodGet, s.handleStatus)
	case r.URL.Path == "/v1/captures":
		s.requireMethod(recorder, r, requestID, http.MethodPost, s.handleCapture)
	case r.URL.Path == "/v1/captures/latest":
		s.requireMethod(recorder, r, requestID, http.MethodGet, s.handleLatest)
	case r.URL.Path == "/v1/captures/latest/content":
		s.requireMethod(recorder, r, requestID, http.MethodGet, s.handleLatestContent)
	case strings.HasPrefix(r.URL.Path, "/v1/captures/"):
		s.handleCaptureResource(recorder, r, requestID)
	default:
		writeError(recorder, requestID, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) requireMethod(w http.ResponseWriter, r *http.Request, requestID, method string, handler func(http.ResponseWriter, *http.Request, string)) {
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeError(w, requestID, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	handler(w, r, requestID)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request, _ string) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, requestID string) {
	captureStatus, err := s.capturer.Status(r.Context())
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	count, err := s.store.Count(r.Context())
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	latest, err := s.store.Latest(r.Context())
	if errors.Is(err, artifact.ErrNotFound) {
		latest = nil
	} else if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Service:       "windows-capture-agent",
		Version:       s.version,
		Capture:       captureStatus,
		ArtifactRoot:  s.store.Root(),
		ArtifactCount: count,
		Latest:        latest,
	})
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request, requestID string) {
	request, err := decodeCaptureRequest(w, r)
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		writeError(w, requestID, http.StatusConflict, "capture_busy", "another capture is already in progress")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
	defer cancel()
	result, err := s.capturer.Capture(ctx, *request.IncludeCursor)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	metadata, err := s.store.Commit(ctx, result)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	s.logger.InfoContext(r.Context(), "capture_committed",
		"request_id", requestID,
		"capture_id", metadata.ID,
		"width", metadata.Width,
		"height", metadata.Height,
		"bytes", metadata.Bytes,
		"hdr", metadata.Monitor.HDR,
		"tone_mapped", metadata.ToneMapped,
		"foreground_process_id", metadata.Foreground.ProcessID,
		"foreground_executable_name", metadata.Foreground.ExecutableName,
	)
	w.Header().Set("Location", "/v1/captures/"+metadata.ID)
	writeJSON(w, http.StatusCreated, metadata)
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request, requestID string) {
	metadata, err := s.store.Latest(r.Context())
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) handleLatestContent(w http.ResponseWriter, r *http.Request, requestID string) {
	metadata, err := s.store.Latest(r.Context())
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	s.serveContent(w, r, requestID, metadata.ID, false)
}

func (s *Server) handleCaptureResource(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, requestID, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/v1/captures/")
	parts := strings.Split(remainder, "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		metadata, err := s.store.Get(r.Context(), parts[0])
		if err != nil {
			s.writeMappedError(w, requestID, err)
			return
		}
		writeJSON(w, http.StatusOK, metadata)
	case len(parts) == 2 && parts[0] != "" && parts[1] == "content":
		s.serveContent(w, r, requestID, parts[0], true)
	default:
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) serveContent(w http.ResponseWriter, r *http.Request, requestID, id string, immutable bool) {
	metadata, content, err := s.store.ReadContent(r.Context(), id)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	etag := `"` + metadata.SHA256 + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("ETag", etag)
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) writeMappedError(w http.ResponseWriter, requestID string, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, requestID, http.StatusGatewayTimeout, "capture_timeout", "capture timed out")
		return
	}
	if errors.Is(err, context.Canceled) {
		writeError(w, requestID, http.StatusRequestTimeout, "request_canceled", "request was canceled")
		return
	}
	if errors.Is(err, artifact.ErrNotFound) {
		writeError(w, requestID, http.StatusNotFound, "artifact_not_found", "artifact not found")
		return
	}
	if errors.Is(err, artifact.ErrCorrupt) {
		writeError(w, requestID, http.StatusInternalServerError, "artifact_store_corrupt", err.Error())
		return
	}
	var captureError *capture.Error
	if errors.As(err, &captureError) {
		status := http.StatusServiceUnavailable
		if captureError.Code == "capture_timeout" {
			status = http.StatusGatewayTimeout
		}
		writeError(w, requestID, status, captureError.Code, captureError.Message)
		return
	}
	writeError(w, requestID, http.StatusInternalServerError, "internal_error", err.Error())
}

func decodeCaptureRequest(w http.ResponseWriter, r *http.Request) (captureRequest, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request captureRequest
	if err := decoder.Decode(&request); err != nil {
		return captureRequest{}, fmt.Errorf("decode JSON body: %w", err)
	}
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return captureRequest{}, errors.New("request body contains multiple JSON values")
	}
	if !errors.Is(err, io.EOF) {
		return captureRequest{}, fmt.Errorf("decode trailing JSON content: %w", err)
	}
	if request.IncludeCursor == nil {
		return captureRequest{}, errors.New("include_cursor is required")
	}
	return request, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, requestID string, status int, code, message string) {
	writeJSON(w, status, ErrorEnvelope{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
