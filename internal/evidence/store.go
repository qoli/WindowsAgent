package evidence

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

type Record struct {
	SchemaVersion        uint32       `json:"schemaVersion"`
	Kind                 string       `json:"kind"`
	ScheduledAt          time.Time    `json:"scheduledAt"`
	ObservedAt           time.Time    `json:"observedAt,omitempty"`
	Sequence             uint64       `json:"sequence,omitempty"`
	ForegroundExecutable string       `json:"foregroundExecutable,omitempty"`
	Gap                  *GapEvidence `json:"gap,omitempty"`
}

type GapEvidence struct {
	Stage string `json:"stage"`
	Error string `json:"error"`
}

type VideoFormat struct {
	Width           int
	Height          int
	FramesPerSecond int
	Bitrate         uint32
}

type SegmentEncoder interface {
	WriteFrame(context.Context, uint64, []byte) error
	Finalize(context.Context) error
}

type EncoderFactory interface {
	Open(string, VideoFormat) (SegmentEncoder, error)
}

type EncoderFactoryFunc func(string, VideoFormat) (SegmentEncoder, error)

func (f EncoderFactoryFunc) Open(path string, format VideoFormat) (SegmentEncoder, error) {
	return f(path, format)
}

type VideoArtifact struct {
	File        string `json:"file"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type SegmentManifest struct {
	SchemaVersion   uint32         `json:"schemaVersion"`
	Start           time.Time      `json:"start"`
	End             time.Time      `json:"end"`
	CommittedAt     time.Time      `json:"committedAt"`
	Codec           string         `json:"codec"`
	Container       string         `json:"container"`
	Width           int            `json:"width"`
	Height          int            `json:"height"`
	FramesPerSecond int            `json:"framesPerSecond"`
	FrameCount      int            `json:"frameCount"`
	GapCount        int            `json:"gapCount"`
	Video           *VideoArtifact `json:"video,omitempty"`
	Records         []Record       `json:"records"`
}

type Manifest struct {
	SchemaVersion uint32            `json:"schemaVersion"`
	From          time.Time         `json:"from"`
	To            time.Time         `json:"to"`
	SnapshotAt    time.Time         `json:"snapshotAt"`
	FrameCount    int               `json:"frameCount"`
	GapCount      int               `json:"gapCount"`
	MissingCount  int               `json:"missingCount"`
	MissingSlots  []time.Time       `json:"missingSlots"`
	Segments      []SegmentManifest `json:"segments"`
}

type Archive struct {
	Path     string
	Filename string
	Manifest Manifest
}

type openSegment struct {
	start       time.Time
	boundaryEnd time.Time
	last        time.Time
	records     []Record
	partialPath string
	encoder     SegmentEncoder
}

type Store struct {
	root       string
	config     Config
	factory    EncoderFactory
	mu         sync.Mutex
	current    *openSegment
	lastSample time.Time
	available  time.Time
}

var ErrRangeNotCommitted = errors.New("evidence range includes uncommitted recording")

func OpenStore(root string, config Config, factory EncoderFactory) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("evidence data root must be absolute")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, errors.New("evidence segment encoder factory is required")
	}
	for _, dir := range []string{filepath.Join(root, "segments"), filepath.Join(root, "exports")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create evidence video directory: %w", err)
		}
	}
	store := &Store{root: root, config: config, factory: factory}
	segments, err := store.listSegments(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("scan committed evidence segments: %w", err)
	}
	for _, segment := range segments {
		if segment.End.After(store.available) {
			store.available = segment.End
		}
	}
	return store, nil
}

func (s *Store) Append(ctx context.Context, sample videocapture.Sample) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if err := sample.Validate(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastSample.IsZero() && !sample.ScheduledAt.After(s.lastSample) {
		return Record{}, errors.New("evidence video samples must advance by UTC slot")
	}
	boundaryStart := alignedSegmentStart(sample.ScheduledAt, s.segmentDuration())
	if s.current != nil && !boundaryStart.Equal(alignedSegmentStart(s.current.start, s.segmentDuration())) {
		if err := s.finalizeLocked(ctx, s.current.boundaryEnd); err != nil {
			return Record{}, err
		}
	}
	if s.current == nil {
		s.current = &openSegment{start: sample.ScheduledAt, boundaryEnd: boundaryStart.Add(s.segmentDuration()), records: make([]Record, 0, s.config.Recording.SegmentSeconds)}
	}
	record := Record{SchemaVersion: SchemaVersion, ScheduledAt: sample.ScheduledAt}
	if sample.Frame == nil {
		record.Kind = "gap"
		record.Gap = &GapEvidence{Stage: sample.Stage, Error: boundedError(sample.Err)}
	} else {
		if sample.Frame.ForegroundExecutable != s.config.TargetExecutable {
			record.Kind = "gap"
			record.Gap = &GapEvidence{Stage: "foreground", Error: fmt.Sprintf("video frame foreground is %q, expected %q", sample.Frame.ForegroundExecutable, s.config.TargetExecutable)}
		} else {
			if s.current.encoder == nil {
				partial, err := s.newPartialPath(s.current.start)
				if err != nil {
					return Record{}, err
				}
				format := VideoFormat{Width: int(s.config.Recording.Width), Height: int(s.config.Recording.Height), FramesPerSecond: int(s.config.Recording.FramesPerSecond), Bitrate: s.config.Recording.Bitrate}
				encoder, err := s.factory.Open(partial, format)
				if err != nil {
					return Record{}, fmt.Errorf("open Media Foundation evidence segment: %w", err)
				}
				s.current.partialPath, s.current.encoder = partial, encoder
			}
			frameIndex := uint64(sample.ScheduledAt.Sub(s.current.start)/time.Second) + 1
			if err := s.current.encoder.WriteFrame(ctx, frameIndex, sample.Frame.Pixels); err != nil {
				return Record{}, fmt.Errorf("encode evidence video frame: %w", err)
			}
			record.Kind = "frame"
			record.ObservedAt = sample.Frame.ObservedAt
			record.Sequence = sample.Frame.Sequence
			record.ForegroundExecutable = sample.Frame.ForegroundExecutable
		}
	}
	s.current.records = append(s.current.records, record)
	s.current.last = sample.ScheduledAt
	s.lastSample = sample.ScheduledAt
	if sample.ScheduledAt.Add(time.Second).Equal(s.current.boundaryEnd) {
		if err := s.finalizeLocked(ctx, s.current.boundaryEnd); err != nil {
			return Record{}, err
		}
	}
	return record, nil
}

func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	return s.finalizeLocked(ctx, s.current.last.Add(time.Second))
}

func (s *Store) AvailableThrough() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.available
}

func (s *Store) finalizeLocked(ctx context.Context, end time.Time) error {
	segment := s.current
	if segment == nil || segment.last.IsZero() || !segment.start.Before(end) {
		return errors.New("evidence segment cannot finalize without ordered samples")
	}
	manifest := SegmentManifest{SchemaVersion: SchemaVersion, Start: segment.start, End: end, CommittedAt: time.Now().UTC(), Codec: s.config.Recording.Codec, Container: s.config.Recording.Container, Width: int(s.config.Recording.Width), Height: int(s.config.Recording.Height), FramesPerSecond: int(s.config.Recording.FramesPerSecond), Records: append([]Record(nil), segment.records...)}
	for _, record := range manifest.Records {
		if record.Kind == "frame" {
			manifest.FrameCount++
		} else {
			manifest.GapCount++
		}
	}
	base := fmt.Sprintf("%s-%s", manifest.Start.Format("20060102T150405Z"), manifest.End.Format("20060102T150405Z"))
	dir := s.segmentDirectory(manifest.Start)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if segment.encoder != nil {
		if err := segment.encoder.Finalize(ctx); err != nil {
			return fmt.Errorf("finalize Media Foundation evidence segment: %w", err)
		}
		if err := syncFile(segment.partialPath); err != nil {
			return err
		}
		finalVideo := filepath.Join(dir, base+".mp4")
		if err := os.Rename(segment.partialPath, finalVideo); err != nil {
			return fmt.Errorf("commit evidence video segment: %w", err)
		}
		bytes, digest, err := fileIntegrity(finalVideo)
		if err != nil {
			return err
		}
		manifest.Video = &VideoArtifact{File: filepath.Base(finalVideo), ContentType: "video/mp4", Bytes: bytes, SHA256: digest}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = writeAtomic(filepath.Join(dir, base+".json"), data, 0o600); err != nil {
		return err
	}
	s.available = manifest.End
	s.current = nil
	return nil
}

func (s *Store) CreateArchive(ctx context.Context, from, to time.Time) (Archive, error) {
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return Archive{}, errors.New("evidence range requires from before to")
	}
	s.mu.Lock()
	if s.current != nil && to.After(s.available) && from.Before(s.current.last.Add(time.Second)) {
		available := s.available
		s.mu.Unlock()
		return Archive{}, fmt.Errorf("%w: availableThrough=%s", ErrRangeNotCommitted, available.Format(time.RFC3339))
	}
	s.mu.Unlock()
	segments, err := s.listSegments(ctx, from, to)
	if err != nil {
		return Archive{}, err
	}
	manifest := Manifest{SchemaVersion: SchemaVersion, From: from, To: to, SnapshotAt: time.Now().UTC(), MissingSlots: make([]time.Time, 0), Segments: segments}
	present := make(map[time.Time]bool)
	for _, segment := range segments {
		for _, record := range segment.Records {
			if record.ScheduledAt.Before(from) || !record.ScheduledAt.Before(to) {
				continue
			}
			present[record.ScheduledAt] = true
			if record.Kind == "frame" {
				manifest.FrameCount++
			} else {
				manifest.GapCount++
			}
		}
	}
	for slot := firstWholeSecond(from); slot.Before(to); slot = slot.Add(time.Second) {
		if !present[slot] {
			manifest.MissingSlots = append(manifest.MissingSlots, slot)
		}
	}
	manifest.MissingCount = len(manifest.MissingSlots)
	file, err := os.CreateTemp(filepath.Join(s.root, "exports"), "evidence-video-*.zip")
	if err != nil {
		return Archive{}, err
	}
	path := file.Name()
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	zw := zip.NewWriter(file)
	for index, segment := range manifest.Segments {
		if segment.Video == nil {
			continue
		}
		sourcePath := filepath.Join(s.segmentDirectory(segment.Start), segment.Video.File)
		if err := copyVerifiedVideo(zw, sourcePath, fmt.Sprintf("segments/%03d-%s", index+1, segment.Video.File), segment.Video); err != nil {
			return Archive{}, err
		}
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
	return Archive{Path: path, Filename: fmt.Sprintf("evidence-video-%s-%s.zip", from.Format("20060102T150405Z"), to.Format("20060102T150405Z")), Manifest: manifest}, nil
}

func (s *Store) listSegments(ctx context.Context, from, to time.Time) ([]SegmentManifest, error) {
	segments := make([]SegmentManifest, 0)
	root := filepath.Join(s.root, "segments")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var segment SegmentManifest
		if err = json.Unmarshal(data, &segment); err != nil {
			return fmt.Errorf("decode evidence video segment %s: %w", entry.Name(), err)
		}
		if err = validateSegment(segment, entry.Name(), s.config.TargetExecutable); err != nil {
			return err
		}
		if from.IsZero() || (segment.Start.Before(to) && segment.End.After(from)) {
			segments = append(segments, segment)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return segments, nil
		}
		return nil, err
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Start.Before(segments[j].Start) })
	for index := 1; index < len(segments); index++ {
		if segments[index].Start.Before(segments[index-1].End) {
			return nil, errors.New("committed evidence video segments overlap")
		}
	}
	return segments, nil
}

func validateSegment(segment SegmentManifest, name, targetExecutable string) error {
	if segment.SchemaVersion != SchemaVersion || segment.Start.IsZero() || !segment.Start.Before(segment.End) || segment.CommittedAt.IsZero() || segment.Codec != "h264" || segment.Container != "mp4" || segment.Width != 1920 || segment.Height != 1080 || segment.FramesPerSecond != 1 {
		return fmt.Errorf("evidence video segment %s identity is invalid", name)
	}
	expected := fmt.Sprintf("%s-%s.json", segment.Start.Format("20060102T150405Z"), segment.End.Format("20060102T150405Z"))
	if name != expected || len(segment.Records) != segment.FrameCount+segment.GapCount || len(segment.Records) != int(segment.End.Sub(segment.Start)/time.Second) {
		return fmt.Errorf("evidence video segment %s counts or filename are invalid", name)
	}
	for index, record := range segment.Records {
		expectedSlot := segment.Start.Add(time.Duration(index) * time.Second)
		if record.SchemaVersion != SchemaVersion || !record.ScheduledAt.Equal(expectedSlot) {
			return fmt.Errorf("evidence video segment %s contains an invalid record", name)
		}
		switch record.Kind {
		case "frame":
			if record.Gap != nil || record.ObservedAt.IsZero() || record.Sequence == 0 || record.ForegroundExecutable != targetExecutable {
				return fmt.Errorf("evidence video segment %s contains an invalid frame record", name)
			}
		case "gap":
			if record.Gap == nil || record.Gap.Stage == "" || record.Gap.Error == "" || !record.ObservedAt.IsZero() || record.Sequence != 0 || record.ForegroundExecutable != "" {
				return fmt.Errorf("evidence video segment %s contains an invalid gap record", name)
			}
		default:
			return fmt.Errorf("evidence video segment %s contains an invalid record kind", name)
		}
	}
	if segment.FrameCount > 0 {
		expectedVideo := fmt.Sprintf("%s-%s.mp4", segment.Start.Format("20060102T150405Z"), segment.End.Format("20060102T150405Z"))
		if segment.Video == nil || segment.Video.ContentType != "video/mp4" || segment.Video.Bytes < 1 || len(segment.Video.SHA256) != 64 || filepath.Base(segment.Video.File) != segment.Video.File || segment.Video.File != expectedVideo {
			return fmt.Errorf("evidence video segment %s video artifact is invalid", name)
		}
	} else if segment.Video != nil {
		return fmt.Errorf("evidence video segment %s cannot contain video without frames", name)
	}
	return nil
}

func (s *Store) segmentDuration() time.Duration {
	return time.Duration(s.config.Recording.SegmentSeconds) * time.Second
}

func (s *Store) segmentDirectory(at time.Time) string {
	at = at.UTC()
	return filepath.Join(s.root, "segments", at.Format("2006"), at.Format("01"), at.Format("02"), at.Format("15"))
}

func (s *Store) newPartialPath(at time.Time) (string, error) {
	dir := s.segmentDirectory(at)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, at.Format("20060102T150405Z")+"-*.partial.mp4")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func alignedSegmentStart(at time.Time, duration time.Duration) time.Time {
	seconds := int64(duration / time.Second)
	unix := at.UTC().Unix()
	return time.Unix(unix-unix%seconds, 0).UTC()
}

func boundedError(cause error) string {
	message := cause.Error()
	if len(message) > 2048 {
		return message[:2048]
	}
	return message
}

func firstWholeSecond(at time.Time) time.Time {
	whole := at.UTC().Truncate(time.Second)
	if whole.Before(at) {
		return whole.Add(time.Second)
	}
	return whole
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func fileIntegrity(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return count, hex.EncodeToString(hash.Sum(nil)), nil
}

func copyVerifiedVideo(zw *zip.Writer, sourcePath, archiveName string, artifact *VideoArtifact) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := zw.CreateHeader(&zip.FileHeader{Name: archiveName, Method: zip.Store})
	if err != nil {
		return err
	}
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(destination, hash), source)
	if err != nil {
		return err
	}
	if count != artifact.Bytes || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return fmt.Errorf("evidence video integrity failed for %s", filepath.Base(sourcePath))
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("evidence artifact already exists: %s", filepath.Base(path))
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
