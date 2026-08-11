// Package evidence owns the independent one-frame-per-second evidence timeline.
package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/qoli/WindowsAgent/internal/frametap"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	RuntimeID     = "wgc-evidence-video-v1"
	SchemaVersion = 3
)

type Config struct {
	SchemaVersion    uint32          `json:"schemaVersion"`
	ModuleID         string          `json:"moduleId"`
	Kind             string          `json:"kind"`
	Runtime          string          `json:"runtime"`
	TargetExecutable string          `json:"targetExecutable"`
	Recording        RecordingConfig `json:"recording"`
	FrameTap         FrameTapConfig  `json:"frameTap"`
	MaxRangeSeconds  uint32          `json:"maxRangeSeconds"`
}

type FrameTapConfig struct {
	Name string `json:"name"`
}

type RecordingConfig struct {
	Width           uint32 `json:"width"`
	Height          uint32 `json:"height"`
	FramesPerSecond uint32 `json:"framesPerSecond"`
	SegmentSeconds  uint32 `json:"segmentSeconds"`
	Codec           string `json:"codec"`
	Container       string `json:"container"`
	Bitrate         uint32 `json:"bitrate"`
	IncludeCursor   bool   `json:"includeCursor"`
}

func LoadConfig(name string) (Config, error) {
	info, err := os.Stat(name)
	if err != nil {
		return Config{}, fmt.Errorf("stat evidence config: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<10 {
		return Config{}, errors.New("evidence config must be a bounded regular file")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	if err := strictjson.Validate(data); err != nil {
		return Config{}, fmt.Errorf("evidence config must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode evidence config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("evidence config must contain exactly one JSON value")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion || c.Kind != "evidence-recorder" || c.Runtime != RuntimeID {
		return errors.New("evidence config identity is invalid")
	}
	if strings.TrimSpace(c.ModuleID) == "" || strings.TrimSpace(c.ModuleID) != c.ModuleID {
		return errors.New("evidence config moduleId is required and must be canonical")
	}
	if strings.TrimSpace(c.TargetExecutable) != c.TargetExecutable || strings.ContainsAny(c.TargetExecutable, `/\\`) || !strings.HasSuffix(strings.ToLower(c.TargetExecutable), ".exe") {
		return errors.New("evidence config targetExecutable must be one executable name ending in .exe")
	}
	if c.Recording.Width != 1920 || c.Recording.Height != 1080 || c.Recording.FramesPerSecond != 1 {
		return errors.New("evidence recording must equal 1920x1080 at 1 FPS")
	}
	if c.Recording.SegmentSeconds < 2 || c.Recording.SegmentSeconds > 60 {
		return errors.New("evidence recording segmentSeconds must be between 2 and 60")
	}
	if c.Recording.Codec != "h264" || c.Recording.Container != "mp4" {
		return errors.New("evidence recording must use h264 in mp4")
	}
	if c.Recording.Bitrate < 1_000_000 || c.Recording.Bitrate > 20_000_000 {
		return errors.New("evidence recording bitrate must be between 1000000 and 20000000")
	}
	if c.Recording.IncludeCursor {
		return errors.New("evidence recording includeCursor must be false")
	}
	if err := frametap.ValidateName(c.FrameTap.Name); err != nil {
		return fmt.Errorf("evidence config frameTap.name: %w", err)
	}
	if c.MaxRangeSeconds < 1 || c.MaxRangeSeconds > 86400 {
		return errors.New("evidence config maxRangeSeconds must be between 1 and 86400")
	}
	return nil
}
