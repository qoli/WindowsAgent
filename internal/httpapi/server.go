package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/actionsequence"
	"github.com/qoli/WindowsAgent/internal/artifact"
	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	maxRequestBody       = 4 << 10
	maxScriptRequestBody = scriptlaunch.MaxRequestBytes + 4<<10
	maxSequenceBody      = 64 << 10
	scriptRequestTimeout = 80 * time.Second
)

type Server struct {
	capturer   capture.Capturer
	store      *artifact.Store
	rules      *rules.Store
	scripts    scriptlaunch.Executor
	actions    ActionService
	timeout    time.Duration
	version    string
	logger     *slog.Logger
	gate       chan struct{}
	scriptGate chan struct{}
	sequence   atomic.Uint64
}

type ActionService interface {
	Invoke(context.Context, scriptlaunch.Invocation) (actionrun.Invocation, error)
	InvokeSequence(context.Context, actionsequence.Request) (actionrun.Invocation, error)
	SequenceToolSchema(string) (actionsequence.ToolSchema, error)
	Get(string) (actionrun.Invocation, error)
	Stop(string) (actionrun.Invocation, error)
	Stream(context.Context, string, uint64, func(eventstream.Event) error) error
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
	IncludeCursor *bool  `json:"include_cursor"`
	Profile       string `json:"profile,omitempty"`
}

type statusResponse struct {
	Service       string             `json:"service"`
	Version       string             `json:"version"`
	Capture       capture.Status     `json:"capture"`
	ArtifactRoot  string             `json:"artifact_root"`
	ArtifactCount int                `json:"artifact_count"`
	Latest        *artifact.Metadata `json:"latest,omitempty"`
}

type scriptCatalogResponse struct {
	RuleID  string                     `json:"ruleId"`
	Scripts []scriptCapabilityResponse `json:"scripts"`
}

type actionCatalogResponse struct {
	RuleID  string         `json:"ruleId"`
	Actions []rules.Action `json:"actions"`
}

type registrationCatalogResponse struct {
	RuleID        string               `json:"ruleId"`
	Registrations []rules.Registration `json:"registrations"`
}

type runtimeCatalogResponse struct {
	RuleID   string                 `json:"ruleId"`
	Runtimes []rules.RuntimeProfile `json:"runtimes"`
}

type scriptCapabilityResponse struct {
	ID           string          `json:"id"`
	Runtime      string          `json:"runtime"`
	Title        string          `json:"title"`
	Version      uint32          `json:"version"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Launcher     scriptLauncher  `json:"launcher"`
}

type scriptLauncher struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	Authentication string `json:"authentication"`
}

func New(
	capturer capture.Capturer,
	store *artifact.Store,
	ruleStore *rules.Store,
	scriptExecutor scriptlaunch.Executor,
	actionService ActionService,
	timeout time.Duration,
	version string,
	logger *slog.Logger,
) (*Server, error) {
	if capturer == nil {
		return nil, errors.New("capturer is required")
	}
	if store == nil {
		return nil, errors.New("artifact store is required")
	}
	if ruleStore == nil {
		return nil, errors.New("rule store is required")
	}
	if scriptExecutor == nil {
		return nil, errors.New("Script executor is required")
	}
	if actionService == nil {
		return nil, errors.New("Action service is required")
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
		capturer:   capturer,
		store:      store,
		rules:      ruleStore,
		scripts:    scriptExecutor,
		actions:    actionService,
		timeout:    timeout,
		version:    version,
		logger:     logger,
		gate:       make(chan struct{}, 1),
		scriptGate: make(chan struct{}, 1),
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
	case strings.HasPrefix(r.URL.Path, "/v1/rules/"):
		s.handleRuleResource(recorder, r, requestID)
	case strings.HasPrefix(r.URL.Path, "/v3/rules/"):
		s.handleActionResource(recorder, r, requestID)
	case strings.HasPrefix(r.URL.Path, "/v4/rules/"):
		s.handleRuntimeResource(recorder, r, requestID)
	case r.URL.Path == "/v1/scripts/run":
		s.requireMethod(recorder, r, requestID, http.MethodPost, s.handleScriptRun)
	case r.URL.Path == "/v1/actions/invoke":
		s.requireMethod(recorder, r, requestID, http.MethodPost, s.handleActionInvoke)
	case r.URL.Path == "/v1/action-sequences/invoke":
		s.requireMethod(recorder, r, requestID, http.MethodPost, s.handleActionSequenceInvoke)
	case strings.HasPrefix(r.URL.Path, "/v1/action-invocations/"):
		s.handleActionInvocationResource(recorder, r, requestID)
	default:
		writeError(recorder, requestID, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) handleRuntimeResource(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, requestID, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/v4/rules/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "runtimes" {
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
		return
	}
	profiles, resolution, err := s.rules.ReadRuntimeProfiles(parts[0])
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, runtimeCatalogResponse{RuleID: resolution.ID, Runtimes: profiles})
}

func (s *Server) handleActionResource(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, requestID, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/v3/rules/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] == "" ||
		(parts[1] != "actions" && parts[1] != "registrations" && parts[1] != "action-sequence-tool") {
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
		return
	}
	if parts[1] == "actions" {
		actions, resolution, err := s.rules.ReadActions(parts[0])
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
			return
		}
		if err != nil {
			s.writeMappedError(w, requestID, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, actionCatalogResponse{RuleID: resolution.ID, Actions: actions})
		return
	}
	if parts[1] == "action-sequence-tool" {
		schema, err := s.actions.SequenceToolSchema(parts[0])
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
			return
		}
		if errors.Is(err, actionsequence.ErrNoAllowedActions) {
			writeError(w, requestID, http.StatusNotFound, "action_sequence_unavailable", "Rule does not declare any Action Sequence candidates")
			return
		}
		if err != nil {
			s.writeMappedError(w, requestID, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, schema)
		return
	}
	registrations, resolution, err := s.rules.ReadRegistrations(parts[0])
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, registrationCatalogResponse{RuleID: resolution.ID, Registrations: registrations})
}

func (s *Server) handleScriptRun(w http.ResponseWriter, r *http.Request, requestID string) {
	invocation, err := decodeScriptInvocation(w, r)
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_script_request", err.Error())
		return
	}
	if _, err := s.rules.ResolvePublicAction(invocation.Capability); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, requestID, http.StatusNotFound, "script_not_found", "Script Action not found")
			return
		}
		s.writeMappedError(w, requestID, fmt.Errorf("resolve public Script Action %q: %w", invocation.Capability, err))
		return
	}
	select {
	case s.scriptGate <- struct{}{}:
		defer func() { <-s.scriptGate }()
	default:
		writeError(w, requestID, http.StatusConflict, "script_busy", "another Script is already running")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), scriptRequestTimeout)
	defer cancel()
	result, err := s.scripts.Run(ctx, invocation)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, requestID, http.StatusGatewayTimeout, "script_timeout", "Script launch timed out")
			return
		}
		if errors.Is(err, context.Canceled) {
			writeError(w, requestID, http.StatusRequestTimeout, "request_canceled", "request was canceled")
			return
		}
		writeError(w, requestID, http.StatusUnprocessableEntity, "script_launch_failed", err.Error())
		return
	}
	s.logger.InfoContext(r.Context(), "script_completed",
		"request_id", requestID,
		"capability", invocation.Capability,
	)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleActionInvoke(w http.ResponseWriter, r *http.Request, requestID string) {
	invocation, err := decodeActionInvocation(w, r)
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_action_request", err.Error())
		return
	}
	result, err := s.actions.Invoke(r.Context(), invocation)
	if err != nil {
		s.writeActionError(w, requestID, err)
		return
	}
	status := http.StatusOK
	if result.Execution.Completion == rules.CompletionStream {
		status = http.StatusAccepted
		w.Header().Set("Location", "/v1/action-invocations/"+result.InvocationID)
	}
	s.logger.InfoContext(r.Context(), "action_invoked",
		"request_id", requestID,
		"invocation_id", result.InvocationID,
		"action_id", result.ActionID,
		"completion", result.Execution.Completion,
		"state", result.State,
	)
	writeJSON(w, status, result)
}

func decodeActionInvocation(w http.ResponseWriter, r *http.Request) (scriptlaunch.Invocation, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScriptRequestBody)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("read JSON body: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("validate JSON body: %w", err)
	}
	var request struct {
		ActionID string         `json:"actionId"`
		Inputs   map[string]any `json:"inputs"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("decode JSON body: %w", err)
	}
	if request.ActionID == "" || strings.TrimSpace(request.ActionID) != request.ActionID {
		return scriptlaunch.Invocation{}, errors.New("actionId is required and must be canonical")
	}
	if request.Inputs == nil {
		return scriptlaunch.Invocation{}, errors.New("inputs object is required")
	}
	return scriptlaunch.Invocation{Capability: request.ActionID, Inputs: request.Inputs}, nil
}

func (s *Server) handleActionSequenceInvoke(w http.ResponseWriter, r *http.Request, requestID string) {
	request, err := decodeActionSequenceRequest(w, r)
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_action_sequence_request", err.Error())
		return
	}
	result, err := s.actions.InvokeSequence(r.Context(), request)
	if err != nil {
		s.writeActionError(w, requestID, err)
		return
	}
	w.Header().Set("Location", "/v1/action-invocations/"+result.InvocationID)
	s.logger.InfoContext(r.Context(), "action_sequence_invoked",
		"request_id", requestID,
		"invocation_id", result.InvocationID,
		"rule_id", result.RuleID,
		"step_count", len(request.Steps),
	)
	writeJSON(w, http.StatusAccepted, result)
}

func decodeActionSequenceRequest(w http.ResponseWriter, r *http.Request) (actionsequence.Request, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSequenceBody)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return actionsequence.Request{}, fmt.Errorf("read JSON body: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return actionsequence.Request{}, fmt.Errorf("validate JSON body: %w", err)
	}
	var request actionsequence.Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return actionsequence.Request{}, fmt.Errorf("decode JSON body: %w", err)
	}
	if err := request.Validate(); err != nil {
		return actionsequence.Request{}, err
	}
	return request, nil
}

func (s *Server) handleActionInvocationResource(w http.ResponseWriter, r *http.Request, requestID string) {
	remainder := strings.TrimPrefix(r.URL.Path, "/v1/action-invocations/")
	parts := strings.Split(remainder, "/")
	switch {
	case len(parts) == 1 && parts[0] != "":
		s.requireMethod(w, r, requestID, http.MethodGet, func(w http.ResponseWriter, _ *http.Request, requestID string) {
			result, err := s.actions.Get(parts[0])
			if err != nil {
				s.writeActionError(w, requestID, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	case len(parts) == 2 && parts[0] != "" && parts[1] == "stop":
		s.requireMethod(w, r, requestID, http.MethodPost, func(w http.ResponseWriter, _ *http.Request, requestID string) {
			result, err := s.actions.Stop(parts[0])
			if err != nil {
				s.writeActionError(w, requestID, err)
				return
			}
			writeJSON(w, http.StatusAccepted, result)
		})
	case len(parts) == 2 && parts[0] != "" && parts[1] == "events":
		s.requireMethod(w, r, requestID, http.MethodGet, func(w http.ResponseWriter, r *http.Request, requestID string) {
			s.handleActionEvents(w, r, requestID, parts[0])
		})
	default:
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
	}
}

var errActionStreamTerminal = errors.New("Action event stream reached terminal event")

func (s *Server) handleActionEvents(w http.ResponseWriter, r *http.Request, requestID, identity string) {
	values, present := r.URL.Query()["after"]
	if !present || len(values) != 1 || values[0] == "" {
		writeError(w, requestID, http.StatusBadRequest, "invalid_after_cursor", "after query parameter is required exactly once")
		return
	}
	after, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_after_cursor", "after query parameter must be an unsigned integer")
		return
	}
	if _, err := s.actions.Get(identity); err != nil {
		s.writeActionError(w, requestID, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	err = s.actions.Stream(r.Context(), identity, after, func(event eventstream.Event) error {
		if err := json.NewEncoder(w).Encode(event); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		switch event.Type {
		case "action.completed", "action.failed", "action.cancelled":
			return errActionStreamTerminal
		default:
			return nil
		}
	})
	if err != nil && !errors.Is(err, errActionStreamTerminal) && !errors.Is(err, context.Canceled) {
		s.logger.ErrorContext(r.Context(), "action_event_stream_failed",
			"request_id", requestID,
			"invocation_id", identity,
			"error", err,
		)
	}
}

func (s *Server) writeActionError(w http.ResponseWriter, requestID string, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, requestID, http.StatusNotFound, "action_not_found", "Action not found")
	case errors.Is(err, actionrun.ErrInvocationNotFound):
		writeError(w, requestID, http.StatusNotFound, "action_invocation_not_found", "Action invocation not found")
	case errors.Is(err, actionrun.ErrNotInterruptible):
		writeError(w, requestID, http.StatusConflict, "action_not_interruptible", err.Error())
	case errors.Is(err, actionrun.ErrRuleSequenceActive), errors.Is(err, actionrun.ErrRuleActionActive):
		writeError(w, requestID, http.StatusConflict, "rule_action_conflict", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, requestID, http.StatusGatewayTimeout, "action_timeout", "Action timed out")
	case errors.Is(err, context.Canceled):
		writeError(w, requestID, http.StatusRequestTimeout, "request_canceled", "request was canceled")
	default:
		writeError(w, requestID, http.StatusUnprocessableEntity, "action_invocation_failed", err.Error())
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
	profile, err := capture.ParseProfile(request.Profile)
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
	captureStarted := time.Now()
	result, err := s.capturer.Capture(ctx, capture.Request{
		Profile: profile, IncludeCursor: *request.IncludeCursor,
	})
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	captureDuration := time.Since(captureStarted)
	result.Rule, err = s.rules.Resolve(result.Foreground.ExecutableName)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	commitStarted := time.Now()
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
		"profile", metadata.Profile,
		"format", metadata.Format,
		"quality", metadata.Quality,
		"chroma_subsampling", metadata.ChromaSubsampling,
		"capture_duration_ms", captureDuration.Milliseconds(),
		"commit_duration_ms", time.Since(commitStarted).Milliseconds(),
		"hdr", metadata.Monitor.HDR,
		"tone_mapped", metadata.ToneMapped,
		"foreground_process_id", metadata.Foreground.ProcessID,
		"foreground_executable_name", metadata.Foreground.ExecutableName,
		"rule_status", metadata.Rule.Status,
		"rule_id", metadata.Rule.ID,
	)
	w.Header().Set("Location", "/v1/captures/"+metadata.ID)
	writeJSON(w, http.StatusCreated, metadata)
}

func (s *Server) handleRuleResource(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, requestID, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/v1/rules/")
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
		return
	}
	switch parts[1] {
	case rules.AgentsFilename:
		s.handleRuleAGENTS(w, r, requestID, parts[0])
	case "scripts":
		s.handleRuleScripts(w, requestID, parts[0])
	default:
		writeError(w, requestID, http.StatusNotFound, "route_not_found", "route not found")
	}
}

func (s *Server) handleRuleAGENTS(w http.ResponseWriter, _ *http.Request, requestID, ruleID string) {
	content, resolution, err := s.rules.ReadAGENTS(ruleID)
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	w.Header().Set("Content-Type", resolution.Agents.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) handleRuleScripts(w http.ResponseWriter, requestID, ruleID string) {
	scripts, resolution, err := s.rules.ReadScripts(ruleID)
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, requestID, http.StatusNotFound, "rule_not_found", "rule not found")
		return
	}
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	response := scriptCatalogResponse{
		RuleID:  resolution.ID,
		Scripts: make([]scriptCapabilityResponse, 0, len(scripts)),
	}
	for _, script := range scripts {
		if script.Runtime != rules.ObservationRuntimeV1 {
			writeError(
				w,
				requestID,
				http.StatusUnprocessableEntity,
				"unsupported_script_runtime",
				fmt.Sprintf("unsupported script runtime %q for capability %q", script.Runtime, script.ID),
			)
			return
		}
		pkg, err := scriptpackage.Load(script.Root, script.ID)
		if err != nil {
			writeError(w, requestID, http.StatusUnprocessableEntity, "script_package_invalid", err.Error())
			return
		}
		response.Scripts = append(response.Scripts, scriptCapabilityResponse{
			ID:           script.ID,
			Runtime:      script.Runtime,
			Title:        pkg.Manifest.Title,
			Version:      pkg.Manifest.Version,
			InputSchema:  json.RawMessage(pkg.InputSchema),
			OutputSchema: json.RawMessage(pkg.OutputSchema),
			Launcher: scriptLauncher{
				Method:         http.MethodPost,
				URL:            "/v1/scripts/run",
				Authentication: "none",
			},
		})
	}
	writeJSON(w, http.StatusOK, response)
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
	w.Header().Set("Content-Type", metadata.ContentType)
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

func decodeScriptInvocation(w http.ResponseWriter, r *http.Request) (scriptlaunch.Invocation, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScriptRequestBody)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("read JSON body: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("validate JSON body: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var invocation scriptlaunch.Invocation
	if err := decoder.Decode(&invocation); err != nil {
		return scriptlaunch.Invocation{}, fmt.Errorf("decode JSON body: %w", err)
	}
	if invocation.Capability == "" ||
		strings.TrimSpace(invocation.Capability) != invocation.Capability {
		return scriptlaunch.Invocation{}, errors.New("capability is required and must be canonical")
	}
	if invocation.Inputs == nil {
		return scriptlaunch.Invocation{}, errors.New("inputs object is required")
	}
	return invocation, nil
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

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
