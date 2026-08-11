package evidence

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Record struct {
	SchemaVersion uint32         `json:"schemaVersion"`
	Kind          string         `json:"kind"`
	ScheduledAt   time.Time      `json:"scheduledAt"`
	CommittedAt   time.Time      `json:"committedAt"`
	Frame         *FrameEvidence `json:"frame,omitempty"`
	Gap           *GapEvidence   `json:"gap,omitempty"`
}
type FrameEvidence struct {
	CaptureID   string    `json:"captureId"`
	ObservedAt  time.Time `json:"observedAt"`
	ContentType string    `json:"contentType"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Bytes       int64     `json:"bytes"`
	SHA256      string    `json:"sha256"`
	File        string    `json:"file"`
}
type GapEvidence struct {
	Stage string `json:"stage"`
	Error string `json:"error"`
}
type Manifest struct {
	SchemaVersion uint32      `json:"schemaVersion"`
	From          time.Time   `json:"from"`
	To            time.Time   `json:"to"`
	SnapshotAt    time.Time   `json:"snapshotAt"`
	FrameCount    int         `json:"frameCount"`
	GapCount      int         `json:"gapCount"`
	MissingCount  int         `json:"missingCount"`
	MissingSlots  []time.Time `json:"missingSlots"`
	Records       []Record    `json:"records"`
}
type Archive struct {
	Path     string
	Filename string
	Manifest Manifest
}

type Store struct {
	root string
	mu   sync.Mutex
}

func OpenStore(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("evidence data root must be absolute")
	}
	for _, dir := range []string{filepath.Join(root, "records"), filepath.Join(root, "exports")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create evidence directory: %w", err)
		}
	}
	return &Store{root: root}, nil
}

func (s *Store) CommitFrame(ctx context.Context, scheduled time.Time, frame Frame) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if frame.ContentType != "image/jpeg" || frame.Width != 1920 || frame.Height != 1080 || len(frame.Content) == 0 {
		return Record{}, errors.New("evidence frame must be a non-empty 1920x1080 JPEG")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(frame.Content))
	if err != nil || config.Width != 1920 || config.Height != 1080 {
		return Record{}, errors.New("evidence frame bytes are not a 1920x1080 JPEG")
	}
	sum := sha256.Sum256(frame.Content)
	digest := hex.EncodeToString(sum[:])
	if frame.SHA256 != "" && frame.SHA256 != digest {
		return Record{}, errors.New("evidence frame SHA-256 does not match content")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, key, err := s.slotPath(scheduled)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Record{}, err
	}
	imageName := key + ".jpg"
	if err := writeAtomic(filepath.Join(dir, imageName), frame.Content, 0o600); err != nil {
		return Record{}, err
	}
	record := Record{SchemaVersion: 1, Kind: "frame", ScheduledAt: scheduled.UTC(), CommittedAt: time.Now().UTC(), Frame: &FrameEvidence{CaptureID: frame.CaptureID, ObservedAt: frame.ObservedAt.UTC(), ContentType: frame.ContentType, Width: frame.Width, Height: frame.Height, Bytes: int64(len(frame.Content)), SHA256: digest, File: imageName}}
	if err := writeRecord(filepath.Join(dir, key+".json"), record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) CommitGap(ctx context.Context, scheduled time.Time, stage string, cause error) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(stage) == "" || cause == nil {
		return Record{}, errors.New("gap stage and error are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, key, err := s.slotPath(scheduled)
	if err != nil {
		return Record{}, err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return Record{}, err
	}
	message := cause.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	record := Record{SchemaVersion: 1, Kind: "gap", ScheduledAt: scheduled.UTC(), CommittedAt: time.Now().UTC(), Gap: &GapEvidence{Stage: stage, Error: message}}
	if err = writeRecord(filepath.Join(dir, key+".json"), record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) slotPath(at time.Time) (string, string, error) {
	at = at.UTC()
	if at.IsZero() || at.Nanosecond() != 0 {
		return "", "", errors.New("evidence slot must be a whole UTC second")
	}
	return filepath.Join(s.root, "records", at.Format("2006"), at.Format("01"), at.Format("02"), at.Format("15")), at.Format("20060102T150405Z"), nil
}

func (s *Store) ListRange(ctx context.Context, from, to time.Time) ([]Record, error) {
	from = from.UTC()
	to = to.UTC()
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, errors.New("evidence range requires from before to")
	}
	seen := map[time.Time]bool{}
	records := make([]Record, 0)
	for hour := from.Truncate(time.Hour); hour.Before(to); hour = hour.Add(time.Hour) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir, _, _ := s.slotPath(hour)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			var record Record
			if err = json.Unmarshal(data, &record); err != nil {
				return nil, fmt.Errorf("decode evidence record %s: %w", entry.Name(), err)
			}
			if err = validateRecord(record); err != nil {
				return nil, fmt.Errorf("validate evidence record %s: %w", entry.Name(), err)
			}
			at := record.ScheduledAt.UTC()
			expectedDir, expectedKey, _ := s.slotPath(at)
			if filepath.Clean(expectedDir) != filepath.Clean(dir) || entry.Name() != expectedKey+".json" {
				return nil, fmt.Errorf("evidence record %s does not match its scheduled slot", entry.Name())
			}
			if at.Before(from) || !at.Before(to) {
				continue
			}
			if seen[at] {
				return nil, fmt.Errorf("duplicate evidence slot %s", at.Format(time.RFC3339))
			}
			seen[at] = true
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ScheduledAt.Before(records[j].ScheduledAt) })
	return records, nil
}

func validateRecord(record Record) error {
	if record.SchemaVersion != 1 || record.ScheduledAt.IsZero() || record.ScheduledAt.Nanosecond() != 0 || record.CommittedAt.IsZero() {
		return errors.New("record identity or timestamps are invalid")
	}
	switch record.Kind {
	case "frame":
		expectedImage := record.ScheduledAt.UTC().Format("20060102T150405Z") + ".jpg"
		if record.Frame == nil || record.Gap != nil || record.Frame.CaptureID == "" || record.Frame.ObservedAt.IsZero() || record.Frame.ContentType != "image/jpeg" || record.Frame.Width != 1920 || record.Frame.Height != 1080 || record.Frame.Bytes < 1 || len(record.Frame.SHA256) != 64 || filepath.Base(record.Frame.File) != record.Frame.File || record.Frame.File != expectedImage {
			return errors.New("frame record is invalid")
		}
	case "gap":
		if record.Gap == nil || record.Frame != nil || strings.TrimSpace(record.Gap.Stage) == "" || record.Gap.Error == "" {
			return errors.New("gap record is invalid")
		}
	default:
		return errors.New("record kind is invalid")
	}
	return nil
}

func (s *Store) CreateArchive(ctx context.Context, from, to time.Time) (Archive, error) {
	records, err := s.ListRange(ctx, from, to)
	if err != nil {
		return Archive{}, err
	}
	manifest := Manifest{SchemaVersion: 1, From: from.UTC(), To: to.UTC(), SnapshotAt: time.Now().UTC(), MissingSlots: make([]time.Time, 0), Records: records}
	present := make(map[time.Time]bool, len(records))
	for _, record := range records {
		present[record.ScheduledAt.UTC()] = true
	}
	for slot := firstWholeSecond(from.UTC()); slot.Before(to.UTC()); slot = slot.Add(time.Second) {
		if !present[slot] {
			manifest.MissingSlots = append(manifest.MissingSlots, slot)
		}
	}
	manifest.MissingCount = len(manifest.MissingSlots)
	if err = os.MkdirAll(filepath.Join(s.root, "exports"), 0o700); err != nil {
		return Archive{}, err
	}
	file, err := os.CreateTemp(filepath.Join(s.root, "exports"), "evidence-*.zip")
	if err != nil {
		return Archive{}, err
	}
	path := file.Name()
	ok := false
	defer func() {
		if !ok {
			file.Close()
			os.Remove(path)
		}
	}()
	zw := zip.NewWriter(file)
	for i := range manifest.Records {
		if err = ctx.Err(); err != nil {
			return Archive{}, err
		}
		record := &manifest.Records[i]
		if record.Kind == "gap" {
			manifest.GapCount++
			continue
		}
		if record.Kind != "frame" || record.Frame == nil {
			return Archive{}, errors.New("invalid evidence record kind")
		}
		manifest.FrameCount++
		dir, _, _ := s.slotPath(record.ScheduledAt)
		source, err := os.Open(filepath.Join(dir, record.Frame.File))
		if err != nil {
			return Archive{}, err
		}
		header := &zip.FileHeader{Name: "frames/" + record.Frame.File, Method: zip.Store}
		header.SetModTime(record.ObservedTime())
		dest, err := zw.CreateHeader(header)
		if err != nil {
			source.Close()
			return Archive{}, err
		}
		hash := sha256.New()
		count, copyErr := io.Copy(io.MultiWriter(dest, hash), source)
		source.Close()
		if copyErr != nil {
			return Archive{}, copyErr
		}
		if count != record.Frame.Bytes || hex.EncodeToString(hash.Sum(nil)) != record.Frame.SHA256 {
			return Archive{}, fmt.Errorf("evidence frame integrity failed for %s", record.Frame.File)
		}
		record.Frame.File = header.Name
	}
	manifestWriter, err := zw.Create("manifest.json")
	if err != nil {
		return Archive{}, err
	}
	encoder := json.NewEncoder(manifestWriter)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(manifest); err != nil {
		return Archive{}, err
	}
	if err = zw.Close(); err != nil {
		return Archive{}, err
	}
	if err = file.Sync(); err != nil {
		return Archive{}, err
	}
	if err = file.Close(); err != nil {
		return Archive{}, err
	}
	ok = true
	return Archive{Path: path, Filename: fmt.Sprintf("evidence-%s-%s.zip", from.UTC().Format("20060102T150405Z"), to.UTC().Format("20060102T150405Z")), Manifest: manifest}, nil
}

func firstWholeSecond(at time.Time) time.Time {
	whole := at.Truncate(time.Second)
	if whole.Before(at) {
		return whole.Add(time.Second)
	}
	return whole
}
func (r Record) ObservedTime() time.Time {
	if r.Frame != nil && !r.Frame.ObservedAt.IsZero() {
		return r.Frame.ObservedAt
	}
	return r.ScheduledAt
}
func writeRecord(path string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("evidence slot already exists: %s", filepath.Base(path))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
