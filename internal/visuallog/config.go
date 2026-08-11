// Package visuallog owns the independent, untrusted screen-description log.
package visuallog

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
	RuntimeID      = "omlx-visual-log-v1"
	SchemaVersion  = 1
	MaxConfigBytes = 64 << 10
)

type Config struct {
	SchemaVersion    uint32       `json:"schemaVersion"`
	ModuleID         string       `json:"moduleId"`
	Kind             string       `json:"kind"`
	Runtime          string       `json:"runtime"`
	TargetExecutable string       `json:"targetExecutable"`
	IntervalMS       int64        `json:"intervalMs"`
	WarmupCalls      uint32       `json:"warmupCalls"`
	CaptureProfile   string       `json:"captureProfile"`
	Prompt           string       `json:"prompt"`
	Model            ModelConfig  `json:"model"`
	Output           OutputConfig `json:"output"`
}

type ModelConfig struct {
	ID          string  `json:"id"`
	MaxTokens   uint32  `json:"maxTokens"`
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
	TopK        uint32  `json:"topK"`
}

type OutputConfig struct {
	Stream              string `json:"stream"`
	ObservationType     string `json:"observationType"`
	FailureType         string `json:"failureType"`
	DescriptionMinWords uint32 `json:"descriptionMinWords"`
	DescriptionMaxWords uint32 `json:"descriptionMaxWords"`
}

func LoadConfig(name string) (Config, error) {
	info, err := os.Stat(name)
	if err != nil {
		return Config{}, fmt.Errorf("stat visual log config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("visual log config must be a regular file")
	}
	if info.Size() < 1 || info.Size() > MaxConfigBytes {
		return Config{}, fmt.Errorf("visual log config size must be between 1 and %d bytes", MaxConfigBytes)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Config{}, fmt.Errorf("read visual log config: %w", err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	if err := strictjson.Validate(data); err != nil {
		return Config{}, fmt.Errorf("visual log config must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode visual log config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("visual log config contains multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode visual log config trailing data: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("visual log config schemaVersion must equal %d", SchemaVersion)
	}
	if c.Kind != "visual-log" {
		return errors.New("visual log config kind must equal visual-log")
	}
	if c.Runtime != RuntimeID {
		return fmt.Errorf("visual log config runtime must equal %s", RuntimeID)
	}
	for name, value := range map[string]string{
		"moduleId":               c.ModuleID,
		"targetExecutable":       c.TargetExecutable,
		"captureProfile":         c.CaptureProfile,
		"prompt":                 c.Prompt,
		"model.id":               c.Model.ID,
		"output.stream":          c.Output.Stream,
		"output.observationType": c.Output.ObservationType,
		"output.failureType":     c.Output.FailureType,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("visual log config %s is required and must be canonical", name)
		}
	}
	if len(c.Prompt) > 4096 {
		return errors.New("visual log config prompt must not exceed 4096 bytes")
	}
	if strings.ContainsAny(c.TargetExecutable, `/\\`) || !strings.HasSuffix(strings.ToLower(c.TargetExecutable), ".exe") {
		return errors.New("visual log config targetExecutable must be one executable name ending in .exe")
	}
	if _, err := capture.ParseProfile(c.CaptureProfile); err != nil {
		return fmt.Errorf("visual log config captureProfile: %w", err)
	}
	if c.IntervalMS < 250 || c.IntervalMS > int64((24*time.Hour)/time.Millisecond) {
		return errors.New("visual log config intervalMs must be between 250 and 86400000")
	}
	if c.WarmupCalls < 1 || c.WarmupCalls > 3 {
		return errors.New("visual log config warmupCalls must be between 1 and 3")
	}
	if c.Model.MaxTokens < 8 || c.Model.MaxTokens > 256 {
		return errors.New("visual log config model.maxTokens must be between 8 and 256")
	}
	if c.Model.Temperature < 0 || c.Model.Temperature > 2 {
		return errors.New("visual log config model.temperature must be between 0 and 2")
	}
	if c.Model.TopP <= 0 || c.Model.TopP > 1 {
		return errors.New("visual log config model.topP must be greater than 0 and at most 1")
	}
	if c.Model.TopK < 1 || c.Model.TopK > 1024 {
		return errors.New("visual log config model.topK must be between 1 and 1024")
	}
	if c.Output.DescriptionMinWords < 1 || c.Output.DescriptionMaxWords > 64 || c.Output.DescriptionMinWords > c.Output.DescriptionMaxWords {
		return errors.New("visual log config output description word bounds must satisfy 1 <= min <= max <= 64")
	}
	return nil
}

func (c Config) Interval() time.Duration {
	return time.Duration(c.IntervalMS) * time.Millisecond
}
