package evidencehttp

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/evidence"
)

type Status struct {
	State            string     `json:"state"`
	StartedAt        time.Time  `json:"startedAt"`
	LastScheduledAt  time.Time  `json:"lastScheduledAt,omitempty"`
	AvailableThrough *time.Time `json:"availableThrough,omitempty"`
	Frames           uint64     `json:"frames"`
	Gaps             uint64     `json:"gaps"`
	TapFailures      uint64     `json:"tapFailures"`
	LastError        string     `json:"lastError,omitempty"`
	LastTapError     string     `json:"lastTapError,omitempty"`
}
type Server struct {
	store    *evidence.Store
	token    string
	maxRange time.Duration
	status   func() Status
	handler  http.Handler
}

func New(store *evidence.Store, token string, maxRange time.Duration, status func() Status) (*Server, error) {
	if store == nil || len(token) < 32 || maxRange <= 0 || status == nil {
		return nil, errors.New("evidence HTTP dependencies are invalid")
	}
	s := &Server{store: store, token: token, maxRange: maxRange, status: status}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/evidence/status", s.auth(s.getStatus))
	mux.HandleFunc("GET /v1/evidence/range", s.auth(s.getRange))
	s.handler = mux
	return s, nil
}
func (s *Server) Handler() http.Handler { return s.handler }
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
func (s *Server) getStatus(w http.ResponseWriter, _ *http.Request) {
	status := s.status()
	if available := s.store.AvailableThrough(); !available.IsZero() {
		status.AvailableThrough = &available
	}
	writeJSON(w, http.StatusOK, status)
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
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": err.Error()}})
}
