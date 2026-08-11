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
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	RuntimeID     = "capture-evidence-v1"
	SchemaVersion = 1
)

type Config struct {
	SchemaVersion    uint32 `json:"schemaVersion"`
	ModuleID         string `json:"moduleId"`
	Kind             string `json:"kind"`
	Runtime          string `json:"runtime"`
	TargetExecutable string `json:"targetExecutable"`
	FramesPerSecond  uint32 `json:"framesPerSecond"`
	CaptureProfile   string `json:"captureProfile"`
	CaptureTimeoutMS int64  `json:"captureTimeoutMs"`
	MaxRangeSeconds  uint32 `json:"maxRangeSeconds"`
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
	profile, err := capture.ParseProfile(c.CaptureProfile)
	if err != nil || profile != capture.Profile1080pJPEG {
		return errors.New("evidence config captureProfile must equal 1080p-jpeg")
	}
	if c.FramesPerSecond != 1 {
		return errors.New("evidence config framesPerSecond must equal 1")
	}
	if c.CaptureTimeoutMS < 250 || c.CaptureTimeoutMS > 5000 {
		return errors.New("evidence config captureTimeoutMs must be between 250 and 5000")
	}
	if c.MaxRangeSeconds < 1 || c.MaxRangeSeconds > 86400 {
		return errors.New("evidence config maxRangeSeconds must be between 1 and 86400")
	}
	return nil
}

func (c Config) CaptureTimeout() time.Duration {
	return time.Duration(c.CaptureTimeoutMS) * time.Millisecond
}
