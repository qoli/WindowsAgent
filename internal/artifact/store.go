package artifact

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

const (
	metadataFilename = "metadata.json"
)

var (
	ErrNotFound = errors.New("artifact not found")
	ErrCorrupt  = errors.New("artifact store is corrupt")
	idPattern   = regexp.MustCompile(`^cap_\d{8}T\d{6}\.\d{9}Z_[0-9a-f]{8}$`)
)

type Metadata struct {
	ID                 string           `json:"id"`
	CreatedAt          time.Time        `json:"created_at"`
	Profile            capture.Profile  `json:"profile"`
	Format             string           `json:"format"`
	ContentType        string           `json:"content_type"`
	Quality            int              `json:"quality,omitempty"`
	ChromaSubsampling  string           `json:"chroma_subsampling,omitempty"`
	Width              int              `json:"width"`
	Height             int              `json:"height"`
	Bytes              int64            `json:"bytes"`
	SHA256             string           `json:"sha256"`
	IncludeCursor      bool             `json:"include_cursor"`
	Monitor            capture.Monitor  `json:"monitor"`
	Foreground         foreground.Info  `json:"foreground"`
	Rule               rules.Resolution `json:"rule"`
	CapturePixelFormat string           `json:"capture_pixel_format"`
	ToneMapped         bool             `json:"tone_mapped"`
	ContentURL         string           `json:"content_url"`
}

type Store struct {
	root      string
	retention int
	now       func() time.Time
	random    io.Reader
}

func New(root string, retention int) (*Store, error) {
	if root == "" {
		return nil, errors.New("artifact root is required")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("artifact root must be absolute: %s", root)
	}
	if retention < 1 {
		return nil, fmt.Errorf("retention must be at least 1: %d", retention)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	store := &Store{
		root:      filepath.Clean(root),
		retention: retention,
		now:       time.Now,
		random:    rand.Reader,
	}
	if _, err := store.scan(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Root() string {
	return s.root
}

func (s *Store) Count(ctx context.Context) (int, error) {
	items, err := s.scan(ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (s *Store) Latest(ctx context.Context) (*Metadata, error) {
	items, err := s.scan(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	metadata := items[len(items)-1]
	return &metadata, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Metadata, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	metadata, err := s.readAndValidate(ctx, id)
	if err != nil {
		return nil, err
	}
	return &metadata, nil
}

func (s *Store) ReadContent(ctx context.Context, id string) (*Metadata, []byte, error) {
	metadata, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	contentName, err := contentFilename(metadata.Format)
	if err != nil {
		return nil, nil, corrupt(id, "resolve capture content filename", err)
	}
	contentPath := filepath.Join(s.root, id, contentName)
	content, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, nil, corrupt(id, "read capture content", err)
	}
	if int64(len(content)) != metadata.Bytes {
		return nil, nil, corrupt(id, "content size does not match metadata", nil)
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != metadata.SHA256 {
		return nil, nil, corrupt(id, "content SHA-256 does not match metadata", nil)
	}
	return metadata, content, nil
}

func (s *Store) Commit(ctx context.Context, result capture.Result) (*Metadata, error) {
	if err := validateResult(result); err != nil {
		return nil, err
	}
	current, err := s.scanIndex(ctx)
	if err != nil {
		return nil, err
	}
	id, createdAt, err := s.newID()
	if err != nil {
		return nil, err
	}
	stageName := ".staging-" + id
	stagePath := filepath.Join(s.root, stageName)
	finalPath := filepath.Join(s.root, id)
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact staging directory: %w", err)
	}
	defer os.RemoveAll(stagePath)

	sum := sha256.Sum256(result.Content)
	metadata := Metadata{
		ID:                 id,
		CreatedAt:          createdAt,
		Profile:            result.Profile,
		Format:             result.Format,
		ContentType:        result.ContentType,
		Quality:            result.Quality,
		ChromaSubsampling:  result.ChromaSubsampling,
		Width:              result.Width,
		Height:             result.Height,
		Bytes:              int64(len(result.Content)),
		SHA256:             hex.EncodeToString(sum[:]),
		IncludeCursor:      result.IncludeCursor,
		Monitor:            result.Monitor,
		Foreground:         result.Foreground,
		Rule:               result.Rule,
		CapturePixelFormat: result.CapturePixelFormat,
		ToneMapped:         result.ToneMapped,
		ContentURL:         "/v1/captures/" + id + "/content",
	}

	contentName, err := contentFilename(result.Format)
	if err != nil {
		return nil, err
	}
	if err := writeSynced(filepath.Join(stagePath, contentName), result.Content); err != nil {
		return nil, fmt.Errorf("write staged capture: %w", err)
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact metadata: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := writeSynced(filepath.Join(stagePath, metadataFilename), metadataBytes); err != nil {
		return nil, fmt.Errorf("write staged metadata: %w", err)
	}

	removeCount := len(current) - s.retention + 1
	for i := 0; i < removeCount; i++ {
		if err := s.remove(current[i].ID); err != nil {
			return nil, fmt.Errorf("apply artifact retention: %w", err)
		}
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		return nil, fmt.Errorf("commit artifact directory: %w", err)
	}
	return &metadata, nil
}

// scanIndex validates the artifact directory and metadata needed for retention
// without re-reading and hashing every retained image on each new capture.
// Full payload validation remains on startup, Count, Latest, Get, and download.
func (s *Store) scanIndex(ctx context.Context) ([]Metadata, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read artifact root: %w", err)
	}
	items := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".staging-") {
			return nil, corrupt(name, "staging directory exists from an incomplete transaction", nil)
		}
		if !entry.IsDir() || !idPattern.MatchString(name) {
			return nil, corrupt(name, "unexpected entry in artifact root", nil)
		}
		metadata, err := s.readMetadataAndStat(name)
		if err != nil {
			return nil, err
		}
		items = append(items, metadata)
	}
	sortMetadata(items)
	return items, nil
}

func (s *Store) scan(ctx context.Context) ([]Metadata, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read artifact root: %w", err)
	}
	items := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".staging-") {
			return nil, corrupt(name, "staging directory exists from an incomplete transaction", nil)
		}
		if !entry.IsDir() || !idPattern.MatchString(name) {
			return nil, corrupt(name, "unexpected entry in artifact root", nil)
		}
		metadata, err := s.readAndValidate(ctx, name)
		if err != nil {
			return nil, err
		}
		items = append(items, metadata)
	}
	sortMetadata(items)
	return items, nil
}

func sortMetadata(items []Metadata) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}

func (s *Store) readAndValidate(ctx context.Context, id string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	dir := filepath.Join(s.root, id)
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{}, ErrNotFound
	}
	if err != nil {
		return Metadata{}, corrupt(id, "stat artifact directory", err)
	}
	if !info.IsDir() {
		return Metadata{}, corrupt(id, "artifact path is not a directory", nil)
	}

	metadata, err := s.readMetadataAndStat(id)
	if err != nil {
		return Metadata{}, err
	}

	contentName, _ := contentFilename(metadata.Format)
	contentPath := filepath.Join(dir, contentName)
	content, err := os.ReadFile(contentPath)
	if err != nil {
		return Metadata{}, corrupt(id, "read capture content", err)
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != metadata.SHA256 {
		return Metadata{}, corrupt(id, "capture content SHA-256 does not match metadata", nil)
	}
	config, decodedFormat, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return Metadata{}, corrupt(id, "capture content is invalid", err)
	}
	if decodedFormat != metadata.Format {
		return Metadata{}, corrupt(id, "capture content format does not match metadata", nil)
	}
	if config.Width != metadata.Width || config.Height != metadata.Height {
		return Metadata{}, corrupt(id, "capture content dimensions do not match metadata", nil)
	}
	return metadata, nil
}

func (s *Store) readMetadataAndStat(id string) (Metadata, error) {
	dir := filepath.Join(s.root, id)
	metadataBytes, err := os.ReadFile(filepath.Join(dir, metadataFilename))
	if err != nil {
		return Metadata{}, corrupt(id, "read metadata.json", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataBytes))
	decoder.DisallowUnknownFields()
	var metadata Metadata
	if err := decoder.Decode(&metadata); err != nil {
		return Metadata{}, corrupt(id, "decode metadata.json", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Metadata{}, corrupt(id, "metadata.json has trailing content", err)
	}
	if err := validateMetadata(id, metadata); err != nil {
		return Metadata{}, corrupt(id, "invalid metadata", err)
	}

	contentName, err := contentFilename(metadata.Format)
	if err != nil {
		return Metadata{}, corrupt(id, "resolve capture content filename", err)
	}
	contentPath := filepath.Join(dir, contentName)
	imageInfo, err := os.Stat(contentPath)
	if err != nil {
		return Metadata{}, corrupt(id, "stat capture content", err)
	}
	if !imageInfo.Mode().IsRegular() || imageInfo.Size() != metadata.Bytes {
		return Metadata{}, corrupt(id, "capture content size or type does not match metadata", nil)
	}
	return metadata, nil
}

func (s *Store) newID() (string, time.Time, error) {
	createdAt := s.now().UTC()
	var suffix [4]byte
	if _, err := io.ReadFull(s.random, suffix[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate capture ID: %w", err)
	}
	id := "cap_" + createdAt.Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(suffix[:])
	return id, createdAt, nil
}

func (s *Store) remove(id string) error {
	if !idPattern.MatchString(id) {
		return corrupt(id, "refusing to remove invalid artifact ID", nil)
	}
	target := filepath.Join(s.root, id)
	relative, err := filepath.Rel(s.root, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return corrupt(id, "artifact removal escaped root", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove artifact %s: %w", id, err)
	}
	return nil
}

func validateResult(result capture.Result) error {
	if err := result.ValidateEncoding(); err != nil {
		return err
	}
	if result.Width <= 0 || result.Height <= 0 {
		return errors.New("capture dimensions must be positive")
	}
	if result.Monitor.DeviceName == "" {
		return errors.New("capture monitor device name is required")
	}
	if err := result.Foreground.Validate(); err != nil {
		return err
	}
	if err := result.Rule.Validate(); err != nil {
		return err
	}
	if result.CapturePixelFormat == "" {
		return errors.New("capture pixel format is required")
	}
	config, decodedFormat, err := image.DecodeConfig(bytes.NewReader(result.Content))
	if err != nil {
		return fmt.Errorf("capture payload is not a valid %s: %w", result.Format, err)
	}
	if decodedFormat != result.Format {
		return fmt.Errorf("capture payload format is %q but result metadata is %q", decodedFormat, result.Format)
	}
	if config.Width != result.Width || config.Height != result.Height {
		return fmt.Errorf(
			"capture content is %dx%d but result metadata is %dx%d",
			config.Width,
			config.Height,
			result.Width,
			result.Height,
		)
	}
	return nil
}

func validateMetadata(id string, metadata Metadata) error {
	switch {
	case metadata.ID != id:
		return fmt.Errorf("metadata ID %q does not match directory %q", metadata.ID, id)
	case metadata.CreatedAt.IsZero():
		return errors.New("created_at is required")
	case metadata.Profile == "":
		return errors.New("profile is required")
	case metadata.Width <= 0 || metadata.Height <= 0:
		return errors.New("width and height must be positive")
	case metadata.Bytes <= 0:
		return errors.New("bytes must be positive")
	case len(metadata.SHA256) != sha256.Size*2:
		return errors.New("sha256 must contain 64 hexadecimal characters")
	case metadata.Monitor.DeviceName == "":
		return errors.New("monitor.device_name is required")
	case metadata.Monitor.Width <= 0 || metadata.Monitor.Height <= 0:
		return errors.New("monitor width and height must be positive")
	case metadata.CapturePixelFormat == "":
		return errors.New("capture_pixel_format is required")
	case metadata.ContentURL != "/v1/captures/"+id+"/content":
		return errors.New("content_url does not match artifact ID")
	case metadata.Monitor.HDR != metadata.ToneMapped:
		return errors.New("HDR captures and tone_mapped must agree")
	}
	if err := metadata.Foreground.Validate(); err != nil {
		return err
	}
	if err := metadata.Rule.Validate(); err != nil {
		return err
	}
	if err := (capture.Result{
		Content: metadataPlaceholder,
		Profile: metadata.Profile, Format: metadata.Format, ContentType: metadata.ContentType,
		FileExtension: extensionForFormat(metadata.Format), Quality: metadata.Quality,
		ChromaSubsampling: metadata.ChromaSubsampling,
	}).ValidateEncoding(); err != nil {
		return err
	}
	if _, err := hex.DecodeString(metadata.SHA256); err != nil {
		return fmt.Errorf("sha256 is not hexadecimal: %w", err)
	}
	const timestampLength = len("20060102T150405.000000000Z")
	createdAt, err := time.Parse("20060102T150405.000000000Z", id[4:4+timestampLength])
	if err != nil {
		return fmt.Errorf("capture ID timestamp is invalid: %w", err)
	}
	if !metadata.CreatedAt.Equal(createdAt) {
		return errors.New("created_at does not match capture ID timestamp")
	}
	return nil
}

var metadataPlaceholder = []byte{1}

func contentFilename(format string) (string, error) {
	switch format {
	case "jpeg":
		return "capture.jpg", nil
	case "png":
		return "capture.png", nil
	default:
		return "", fmt.Errorf("unsupported capture format %q", format)
	}
}

func extensionForFormat(format string) string {
	switch format {
	case "jpeg":
		return ".jpg"
	case "png":
		return ".png"
	default:
		return ""
	}
}

func writeSynced(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected second JSON value")
	}
	return err
}

func corrupt(id, message string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s: %s", ErrCorrupt, id, message)
	}
	return fmt.Errorf("%w: %s: %s: %v", ErrCorrupt, id, message, err)
}
