// Package eventstream owns the durable, append-only event journal shared by
// game-scoped producers, reactors, actions, and model consumers.
package eventstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	SchemaVersion      = 1
	JournalFilename    = "events.jsonl"
	MaxEventBytes      = 1 << 20
	DefaultReplayLimit = 256
	MaxReplayLimit     = 4096
)

var ErrCursorAhead = errors.New("event cursor is ahead of the journal")

type Source struct {
	ModuleID   string `json:"moduleId"`
	InstanceID string `json:"instanceId"`
	Runtime    string `json:"runtime"`
}

type Foreground struct {
	ExecutableName string `json:"executableName"`
	Revision       uint64 `json:"revision"`
}

type Artifact struct {
	ID        string `json:"id"`
	MediaType string `json:"mediaType"`
}

type AppendRequest struct {
	SessionID     string          `json:"sessionId"`
	Stream        string          `json:"stream"`
	Type          string          `json:"type"`
	ObservedAt    time.Time       `json:"observedAt"`
	Source        Source          `json:"source"`
	Foreground    Foreground      `json:"foreground"`
	CorrelationID string          `json:"correlationId,omitempty"`
	CausationID   string          `json:"causationId,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Artifacts     []Artifact      `json:"artifacts,omitempty"`
}

type Event struct {
	SchemaVersion uint32          `json:"schemaVersion"`
	Sequence      uint64          `json:"sequence"`
	EventID       string          `json:"eventId"`
	SessionID     string          `json:"sessionId"`
	Stream        string          `json:"stream"`
	Type          string          `json:"type"`
	ObservedAt    time.Time       `json:"observedAt"`
	CommittedAt   time.Time       `json:"committedAt"`
	Source        Source          `json:"source"`
	Foreground    Foreground      `json:"foreground"`
	CorrelationID string          `json:"correlationId,omitempty"`
	CausationID   string          `json:"causationId,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	Artifacts     []Artifact      `json:"artifacts,omitempty"`
}

type TimeRangeResult struct {
	Events       []Event
	NextCursor   uint64
	LastSequence uint64
	Complete     bool
}

type Store struct {
	mu           sync.Mutex
	root         string
	path         string
	file         *os.File
	lastSequence uint64
	offsets      []int64
	fatalErr     error
	closed       bool
	notify       chan struct{}
	now          func() time.Time
	random       io.Reader
}

func Open(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("event journal root must be an absolute path")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create event journal root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve event journal root: %w", err)
	}
	path := filepath.Join(canonicalRoot, JournalFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open event journal: %w", err)
	}
	var offsets []int64
	last, err := scanJournal(file, func(_ Event, offset int64) error {
		offsets = append(offsets, offset)
		return nil
	})
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("validate event journal: %w", err)
	}
	return &Store{
		root:         canonicalRoot,
		path:         path,
		file:         file,
		lastSequence: last,
		offsets:      offsets,
		notify:       make(chan struct{}),
		now:          time.Now,
		random:       rand.Reader,
	}, nil
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Store) LastSequence() (uint64, error) {
	if s == nil {
		return 0, errors.New("event journal store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return 0, err
	}
	return s.lastSequence, nil
}

func (s *Store) Append(ctx context.Context, request AppendRequest) (Event, error) {
	if s == nil {
		return Event{}, errors.New("event journal store is required")
	}
	if ctx == nil {
		return Event{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if err := validateAppendRequest(request); err != nil {
		return Event{}, fmt.Errorf("validate event append request: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return Event{}, err
	}
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	eventID, err := newEventID(s.random)
	if err != nil {
		return Event{}, fmt.Errorf("create event ID: %w", err)
	}
	event := Event{
		SchemaVersion: SchemaVersion,
		Sequence:      s.lastSequence + 1,
		EventID:       eventID,
		SessionID:     request.SessionID,
		Stream:        request.Stream,
		Type:          request.Type,
		ObservedAt:    request.ObservedAt,
		CommittedAt:   s.now().UTC(),
		Source:        request.Source,
		Foreground:    request.Foreground,
		CorrelationID: request.CorrelationID,
		CausationID:   request.CausationID,
		Payload:       append(json.RawMessage(nil), request.Payload...),
		Artifacts:     append([]Artifact(nil), request.Artifacts...),
	}
	if err := validateEvent(event); err != nil {
		return Event{}, fmt.Errorf("validate committed event: %w", err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return Event{}, fmt.Errorf("encode event: %w", err)
	}
	if len(encoded)+1 > MaxEventBytes {
		return Event{}, fmt.Errorf("event exceeds %d bytes", MaxEventBytes)
	}
	encoded = append(encoded, '\n')
	offset, err := s.file.Seek(0, io.SeekEnd)
	if err != nil {
		return Event{}, s.poison(fmt.Errorf("seek event journal end: %w", err))
	}
	if _, err := s.file.Write(encoded); err != nil {
		return Event{}, s.poison(fmt.Errorf("append event record: %w", err))
	}
	if err := s.file.Sync(); err != nil {
		return Event{}, s.poison(fmt.Errorf("sync event record: %w", err))
	}
	s.lastSequence = event.Sequence
	s.offsets = append(s.offsets, offset)
	close(s.notify)
	s.notify = make(chan struct{})
	return event, nil
}

func (s *Store) WaitAfter(ctx context.Context, after uint64, limit int) ([]Event, error) {
	if s == nil {
		return nil, errors.New("event journal store is required")
	}
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	for {
		s.mu.Lock()
		if err := s.available(); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		last := s.lastSequence
		if after > last {
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: cursor=%d lastSequence=%d", ErrCursorAhead, after, last)
		}
		if after < last {
			s.mu.Unlock()
			return s.ReadAfter(ctx, after, limit)
		}
		notify := s.notify
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-notify:
		}
	}
}

func (s *Store) ReadAfter(ctx context.Context, after uint64, limit int) ([]Event, error) {
	if s == nil {
		return nil, errors.New("event journal store is required")
	}
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if limit < 1 || limit > MaxReplayLimit {
		return nil, fmt.Errorf("event replay limit must be between 1 and %d", MaxReplayLimit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		return nil, err
	}
	if after > s.lastSequence {
		return nil, fmt.Errorf("%w: cursor=%d lastSequence=%d", ErrCursorAhead, after, s.lastSequence)
	}
	if after == s.lastSequence {
		return []Event{}, nil
	}
	if after >= uint64(len(s.offsets)) {
		return nil, fmt.Errorf("event journal index is missing sequence %d", after+1)
	}
	if _, err := s.file.Seek(s.offsets[after], io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek event sequence %d: %w", after+1, err)
	}
	events := make([]Event, 0, limit)
	reader := bufio.NewReaderSize(s.file, 64<<10)
	for expected := after + 1; len(events) < limit; expected++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) == 0 {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read event sequence %d: %w", expected, err)
		}
		event, err := decodeJournalRecord(line, expected)
		if err != nil {
			return nil, fmt.Errorf("replay event journal: %w", err)
		}
		events = append(events, event)
	}
	return events, nil
}

// ReadTimeRange returns events whose producer observation time is in [from, to).
// The cursor remains the durable pagination authority; timestamps only filter.
func (s *Store) ReadTimeRange(ctx context.Context, after uint64, from, to time.Time, stream string, limit int) (TimeRangeResult, error) {
	if s == nil {
		return TimeRangeResult{}, errors.New("event journal store is required")
	}
	if ctx == nil {
		return TimeRangeResult{}, errors.New("context is required")
	}
	if from.IsZero() || to.IsZero() || from.Location() != time.UTC || to.Location() != time.UTC || !from.Before(to) {
		return TimeRangeResult{}, errors.New("event time range requires non-zero UTC from before to")
	}
	if err := validateIdentifier("stream", stream, true); err != nil {
		return TimeRangeResult{}, err
	}
	if limit < 1 || limit > MaxReplayLimit {
		return TimeRangeResult{}, fmt.Errorf("event time range limit must be between 1 and %d", MaxReplayLimit)
	}
	last, err := s.LastSequence()
	if err != nil {
		return TimeRangeResult{}, err
	}
	if after > last {
		return TimeRangeResult{}, fmt.Errorf("%w: cursor=%d lastSequence=%d", ErrCursorAhead, after, last)
	}
	result := TimeRangeResult{Events: make([]Event, 0, limit), NextCursor: after, LastSequence: last, Complete: true}
	cursor := after
	for cursor < last {
		batchLimit := DefaultReplayLimit
		if remaining := int(last - cursor); remaining < batchLimit {
			batchLimit = remaining
		}
		batch, err := s.ReadAfter(ctx, cursor, batchLimit)
		if err != nil {
			return TimeRangeResult{}, err
		}
		if len(batch) == 0 {
			break
		}
		for _, event := range batch {
			cursor = event.Sequence
			if event.Stream != stream || event.ObservedAt.Before(from) || !event.ObservedAt.Before(to) {
				continue
			}
			if len(result.Events) == limit {
				result.Complete = false
				return result, nil
			}
			result.Events = append(result.Events, event)
			result.NextCursor = event.Sequence
		}
	}
	if len(result.Events) == 0 {
		result.NextCursor = last
	}
	return result, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.notify != nil {
		close(s.notify)
		s.notify = nil
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close event journal: %w", err)
	}
	return nil
}

func (s *Store) available() error {
	if s.closed {
		return errors.New("event journal is closed")
	}
	if s.fatalErr != nil {
		return fmt.Errorf("event journal is unavailable after a write failure: %w", s.fatalErr)
	}
	return nil
}

func (s *Store) poison(err error) error {
	if s.fatalErr == nil {
		s.fatalErr = err
		if s.notify != nil {
			close(s.notify)
			s.notify = nil
		}
	}
	return err
}

func scanJournal(file *os.File, visit func(Event, int64) error) (uint64, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	var last uint64
	var offset int64
	for recordNumber := uint64(1); ; recordNumber++ {
		recordOffset := offset
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			if len(line) != 0 {
				return 0, fmt.Errorf("event record %d is not newline terminated", recordNumber)
			}
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read event record %d: %w", recordNumber, err)
		}
		event, err := decodeJournalRecord(line, last+1)
		if err != nil {
			return 0, fmt.Errorf("event record %d: %w", recordNumber, err)
		}
		last = event.Sequence
		if visit != nil {
			if err := visit(event, recordOffset); err != nil {
				return 0, err
			}
		}
		offset += int64(len(line))
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	return last, nil
}

func decodeJournalRecord(line []byte, expectedSequence uint64) (Event, error) {
	if len(line) > MaxEventBytes {
		return Event{}, fmt.Errorf("event record exceeds %d bytes", MaxEventBytes)
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return Event{}, errors.New("event record is not newline terminated")
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	if len(line) == 0 {
		return Event{}, errors.New("event record is empty")
	}
	var event Event
	if err := decodeStrict(line, &event); err != nil {
		return Event{}, fmt.Errorf("decode event record: %w", err)
	}
	if err := validateEvent(event); err != nil {
		return Event{}, fmt.Errorf("validate event record: %w", err)
	}
	if event.Sequence != expectedSequence {
		return Event{}, fmt.Errorf("event sequence is %d, expected %d", event.Sequence, expectedSequence)
	}
	return event, nil
}

func validateAppendRequest(request AppendRequest) error {
	if err := validateIdentifier("sessionId", request.SessionID, false); err != nil {
		return err
	}
	if err := validateIdentifier("stream", request.Stream, true); err != nil {
		return err
	}
	if err := validateIdentifier("type", request.Type, true); err != nil {
		return err
	}
	if request.ObservedAt.IsZero() || request.ObservedAt.Location() != time.UTC {
		return errors.New("observedAt must be a non-zero UTC timestamp")
	}
	if err := validateSource(request.Source); err != nil {
		return err
	}
	if err := validateForeground(request.Foreground); err != nil {
		return err
	}
	if request.CorrelationID != "" {
		if err := validateIdentifier("correlationId", request.CorrelationID, true); err != nil {
			return err
		}
	}
	if request.CausationID != "" {
		if err := validateIdentifier("causationId", request.CausationID, true); err != nil {
			return err
		}
	}
	if len(request.Payload) == 0 || !json.Valid(request.Payload) {
		return errors.New("payload must be valid JSON")
	}
	if err := strictjson.Validate(request.Payload); err != nil {
		return fmt.Errorf("payload must be strict JSON: %w", err)
	}
	for index, artifact := range request.Artifacts {
		if err := validateIdentifier(fmt.Sprintf("artifacts[%d].id", index), artifact.ID, true); err != nil {
			return err
		}
		if strings.TrimSpace(artifact.MediaType) == "" || strings.TrimSpace(artifact.MediaType) != artifact.MediaType {
			return fmt.Errorf("artifacts[%d].mediaType is required and must be canonical", index)
		}
	}
	return nil
}

func validateEvent(event Event) error {
	if event.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion must equal %d", SchemaVersion)
	}
	if event.Sequence == 0 {
		return errors.New("sequence must be positive")
	}
	if !strings.HasPrefix(event.EventID, "evt_") {
		return errors.New("eventId must have the evt_ prefix")
	}
	if err := validateIdentifier("eventId", event.EventID, true); err != nil {
		return err
	}
	if event.CommittedAt.IsZero() || event.CommittedAt.Location() != time.UTC {
		return errors.New("committedAt must be a non-zero UTC timestamp")
	}
	return validateAppendRequest(AppendRequest{
		SessionID:     event.SessionID,
		Stream:        event.Stream,
		Type:          event.Type,
		ObservedAt:    event.ObservedAt,
		Source:        event.Source,
		Foreground:    event.Foreground,
		CorrelationID: event.CorrelationID,
		CausationID:   event.CausationID,
		Payload:       event.Payload,
		Artifacts:     event.Artifacts,
	})
}

func validateSource(source Source) error {
	if err := validateIdentifier("source.moduleId", source.ModuleID, true); err != nil {
		return err
	}
	if err := validateIdentifier("source.instanceId", source.InstanceID, true); err != nil {
		return err
	}
	return validateIdentifier("source.runtime", source.Runtime, true)
}

func validateForeground(foreground Foreground) error {
	if foreground.ExecutableName == "" || strings.ContainsAny(foreground.ExecutableName, `/\`) ||
		!strings.HasSuffix(strings.ToLower(foreground.ExecutableName), ".exe") {
		return errors.New("foreground.executableName must be a single executable name ending in .exe")
	}
	if foreground.Revision == 0 {
		return errors.New("foreground.revision must be positive")
	}
	return nil
}

func validateIdentifier(field, value string, structured bool) error {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is required, canonical, and at most 256 bytes", field)
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' ||
			structured && (char == '/' || char == '.') {
			continue
		}
		return fmt.Errorf("%s contains unsupported character %q", field, char)
	}
	return nil
}

func newEventID(reader io.Reader) (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(data), nil
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return err
	}
	return nil
}
