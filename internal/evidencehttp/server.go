package evidencehttp

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/evidence"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	maxContactSheetRequestBytes = 16 << 10
	maxRunRequestBytes          = 4096
)

type Status struct {
	evidence.RunStatus
	AvailableThrough *time.Time `json:"availableThrough,omitempty"`
}

type RunControl interface {
	Start(evidence.RunRequest) (evidence.RunStatus, error)
	Status() evidence.RunStatus
	RunStatus(string) (evidence.RunStatus, error)
}

type Server struct {
	store      *evidence.Store
	decoder    evidence.VideoFrameDecoder
	token      string
	maxRange   time.Duration
	controller RunControl
	handler    http.Handler
}

func New(store *evidence.Store, decoder evidence.VideoFrameDecoder, token string, maxRange time.Duration, controller RunControl) (*Server, error) {
	if store == nil || decoder == nil || len(token) < 32 || maxRange <= 0 || controller == nil {
		return nil, errors.New("evidence HTTP dependencies are invalid")
	}
	s := &Server{store: store, decoder: decoder, token: token, maxRange: maxRange, controller: controller}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/evidence/status", s.auth(s.getStatus))
	mux.HandleFunc("POST /v1/evidence/runs", s.auth(s.postRun))
	mux.HandleFunc("GET /v1/evidence/runs/{runId}", s.auth(s.getRun))
	mux.HandleFunc("GET /v1/evidence/range", s.auth(s.getRange))
	mux.HandleFunc("POST /v1/evidence/contact-sheet", s.auth(s.postContactSheet))
	s.handler = mux
	return s, nil
}

type contactSheetRequest struct {
	From            string `json:"from"`
	Columns         uint32 `json:"columns"`
	Rows            uint32 `json:"rows"`
	IntervalSeconds uint32 `json:"intervalSeconds"`
}

func (s *Server) postContactSheet(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		writeError(w, http.StatusBadRequest, "EVIDENCE_CONTACT_SHEET_INVALID", errors.New("query parameters are not accepted"))
		return
	}
	request, err := decodeContactSheetRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "EVIDENCE_CONTACT_SHEET_INVALID", err)
		return
	}
	from, err := parseUTC(request.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, "EVIDENCE_CONTACT_SHEET_INVALID", err)
		return
	}
	sheet, err := s.store.CreateContactSheet(r.Context(), evidence.ContactSheetSpec{
		From:            from,
		Columns:         request.Columns,
		Rows:            request.Rows,
		IntervalSeconds: request.IntervalSeconds,
	}, s.decoder)
	switch {
	case errors.Is(err, evidence.ErrContactSheetInvalid):
		writeError(w, http.StatusBadRequest, "EVIDENCE_CONTACT_SHEET_INVALID", err)
		return
	case errors.Is(err, evidence.ErrContactSheetTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "EVIDENCE_CONTACT_SHEET_TOO_LARGE", err)
		return
	case errors.Is(err, evidence.ErrRangeNotCommitted):
		writeError(w, http.StatusConflict, "EVIDENCE_RANGE_NOT_COMMITTED", err)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "EVIDENCE_CONTACT_SHEET_FAILED", err)
		return
	}
	w.Header().Set("Content-Type", sheet.ContentType)
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(sheet.Filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Evidence-Contact-Sheet-Schema", strconv.FormatUint(uint64(sheet.SchemaVersion), 10))
	w.Header().Set("X-Evidence-From", sheet.From.Format(time.RFC3339))
	w.Header().Set("X-Evidence-To", sheet.To.Format(time.RFC3339))
	w.Header().Set("X-Evidence-Cells", strconv.FormatUint(uint64(sheet.CellCount), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(sheet.Content)
}

func decodeContactSheetRequest(w http.ResponseWriter, r *http.Request) (contactSheetRequest, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return contactSheetRequest{}, errors.New("Content-Type must equal application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContactSheetRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return contactSheetRequest{}, fmt.Errorf("read contact sheet request: %w", err)
	}
	if err = strictjson.Validate(data); err != nil {
		return contactSheetRequest{}, fmt.Errorf("contact sheet request must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request contactSheetRequest
	if err = decoder.Decode(&request); err != nil {
		return contactSheetRequest{}, fmt.Errorf("decode contact sheet request: %w", err)
	}
	return request, nil
}
func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
func (s *Server) getStatus(w http.ResponseWriter, _ *http.Request) {
	status := Status{RunStatus: s.controller.Status()}
	if available := s.store.AvailableThrough(); !available.IsZero() {
		status.AvailableThrough = &available
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) postRun(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RUN_REQUEST_INVALID", errors.New("query parameters are not accepted"))
		return
	}
	request, err := decodeRunRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RUN_REQUEST_INVALID", err)
		return
	}
	status, err := s.controller.Start(request)
	switch {
	case errors.Is(err, evidence.ErrDurationInvalid):
		writeError(w, http.StatusBadRequest, "EVIDENCE_DURATION_INVALID", err)
		return
	case errors.Is(err, evidence.ErrRunActive):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     map[string]string{"code": "EVIDENCE_RUN_ACTIVE", "message": err.Error()},
			"activeRun": status,
		})
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "EVIDENCE_RUN_START_FAILED", err)
		return
	}
	w.Header().Set("Location", "/v1/evidence/runs/"+url.PathEscape(status.RunID))
	writeJSON(w, http.StatusAccepted, status)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RUN_REQUEST_INVALID", errors.New("query parameters are not accepted"))
		return
	}
	status, err := s.controller.RunStatus(r.PathValue("runId"))
	if errors.Is(err, evidence.ErrRunNotFound) {
		writeError(w, http.StatusNotFound, "EVIDENCE_RUN_NOT_FOUND", err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVIDENCE_RUN_STATUS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func decodeRunRequest(w http.ResponseWriter, r *http.Request) (evidence.RunRequest, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return evidence.RunRequest{}, errors.New("Content-Type must equal application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunRequestBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return evidence.RunRequest{}, fmt.Errorf("read Evidence run request: %w", err)
	}
	if err = strictjson.Validate(data); err != nil {
		return evidence.RunRequest{}, fmt.Errorf("Evidence run request must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire struct {
		DurationSeconds json.RawMessage `json:"durationSeconds"`
	}
	if err = decoder.Decode(&wire); err != nil {
		return evidence.RunRequest{}, fmt.Errorf("decode Evidence run request: %w", err)
	}
	if len(wire.DurationSeconds) == 0 {
		return evidence.RunRequest{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(wire.DurationSeconds), []byte("null")) {
		return evidence.RunRequest{}, errors.New("durationSeconds must be an integer when provided")
	}
	var durationSeconds uint32
	if err = json.Unmarshal(wire.DurationSeconds, &durationSeconds); err != nil {
		return evidence.RunRequest{}, errors.New("durationSeconds must be an integer when provided")
	}
	return evidence.RunRequest{DurationSeconds: &durationSeconds}, nil
}

func (s *Server) getRange(w http.ResponseWriter, r *http.Request) {
	if err := rejectUnknown(r.URL.Query(), "from", "to"); err != nil {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RANGE_INVALID", err)
		return
	}
	from, err := parseUTC(r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RANGE_INVALID", err)
		return
	}
	to, err := parseUTC(r.URL.Query().Get("to"))
	if err != nil || !from.Before(to) {
		writeError(w, http.StatusBadRequest, "EVIDENCE_RANGE_INVALID", errors.New("from must be before to"))
		return
	}
	if to.Sub(from) > s.maxRange {
		writeError(w, http.StatusRequestEntityTooLarge, "EVIDENCE_RANGE_TOO_LARGE", fmt.Errorf("range exceeds %s", s.maxRange))
		return
	}
	archive, err := s.store.CreateArchive(r.Context(), from, to)
	if errors.Is(err, evidence.ErrRangeNotCommitted) {
		writeError(w, http.StatusConflict, "EVIDENCE_RANGE_NOT_COMMITTED", err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVIDENCE_ARCHIVE_FAILED", err)
		return
	}
	defer os.Remove(archive.Path)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(archive.Filename))
	http.ServeFile(w, r, archive.Path)
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(s.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", errors.New("valid bearer token required"))
			return
		}
		next(w, r)
	}
}
func parseUTC(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("from and to are required")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, errors.New("timestamps must use RFC3339 UTC with Z")
	}
	return parsed.UTC(), nil
}
func rejectUnknown(values url.Values, allowed ...string) error {
	set := map[string]bool{}
	for _, v := range allowed {
		set[v] = true
	}
	for key, list := range values {
		if !set[key] || len(list) != 1 {
			return fmt.Errorf("unexpected or repeated query parameter %q", key)
		}
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
}
