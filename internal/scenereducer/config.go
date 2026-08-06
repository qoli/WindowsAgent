// Package scenereducer converts high-frequency ScreenParser detections into a
// durable, lower-frequency scene timeline without interpreting page text.
package scenereducer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const RuntimeID = "screen-scene-reducer-v1"

type Config struct {
	SchemaVersion    uint32        `json:"schemaVersion"`
	ModuleID         string        `json:"moduleId"`
	Kind             string        `json:"kind"`
	Runtime          string        `json:"runtime"`
	TargetExecutable string        `json:"targetExecutable"`
	Input            InputConfig   `json:"input"`
	Output           OutputConfig  `json:"output"`
	Reducer          ReducerConfig `json:"reducer"`
}

type InputConfig struct {
	ModuleID      string `json:"moduleId"`
	Stream        string `json:"stream"`
	ParsedType    string `json:"parsedType"`
	LifecycleType string `json:"lifecycleType"`
	FailureType   string `json:"failureType"`
}

type OutputConfig struct {
	Stream                string `json:"stream"`
	SceneChangedType      string `json:"sceneChangedType"`
	SceneStableType       string `json:"sceneStableType"`
	ForegroundChangedType string `json:"foregroundChangedType"`
	SourceFailureType     string `json:"sourceFailureType"`
}

type ReducerConfig struct {
	PositionQuantum  float64 `json:"positionQuantum"`
	ChangeThreshold  float64 `json:"changeThreshold"`
	StableIntervalMS int64   `json:"stableIntervalMs"`
	MaxRegions       int     `json:"maxRegions"`
}

func LoadConfig(name string) (Config, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return Config{}, fmt.Errorf("read reducer config: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return Config{}, fmt.Errorf("reducer config must be strict JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode reducer config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("reducer config contains multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode trailing reducer config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("reducer config schemaVersion must equal 1")
	}
	if c.Kind != "reactor" {
		return errors.New("reducer config kind must equal reactor")
	}
	if c.Runtime != RuntimeID {
		return fmt.Errorf("reducer config runtime must equal %s", RuntimeID)
	}
	for name, value := range map[string]string{
		"moduleId":                     c.ModuleID,
		"targetExecutable":             c.TargetExecutable,
		"input.moduleId":               c.Input.ModuleID,
		"input.stream":                 c.Input.Stream,
		"input.parsedType":             c.Input.ParsedType,
		"input.lifecycleType":          c.Input.LifecycleType,
		"input.failureType":            c.Input.FailureType,
		"output.stream":                c.Output.Stream,
		"output.sceneChangedType":      c.Output.SceneChangedType,
		"output.sceneStableType":       c.Output.SceneStableType,
		"output.foregroundChangedType": c.Output.ForegroundChangedType,
		"output.sourceFailureType":     c.Output.SourceFailureType,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("reducer config %s is required and must be canonical", name)
		}
	}
	if !strings.HasSuffix(strings.ToLower(c.TargetExecutable), ".exe") || strings.ContainsAny(c.TargetExecutable, `/\\`) {
		return errors.New("reducer config targetExecutable must be one executable name ending in .exe")
	}
	if c.Input.Stream == c.Output.Stream {
		return errors.New("reducer input and output streams must differ")
	}
	if c.Reducer.PositionQuantum <= 0 || c.Reducer.PositionQuantum > 0.25 {
		return errors.New("reducer positionQuantum must be greater than 0 and at most 0.25")
	}
	if c.Reducer.ChangeThreshold <= 0 || c.Reducer.ChangeThreshold > 1 {
		return errors.New("reducer changeThreshold must be greater than 0 and at most 1")
	}
	if c.Reducer.StableIntervalMS < 1000 || c.Reducer.StableIntervalMS > int64((24*time.Hour)/time.Millisecond) {
		return errors.New("reducer stableIntervalMs must be between 1000 and 86400000")
	}
	if c.Reducer.MaxRegions < 1 || c.Reducer.MaxRegions > 64 {
		return errors.New("reducer maxRegions must be between 1 and 64")
	}
	return nil
}

func (c Config) StableInterval() time.Duration {
	return time.Duration(c.Reducer.StableIntervalMS) * time.Millisecond
}
