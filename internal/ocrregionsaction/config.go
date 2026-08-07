// Package ocrregionsaction owns fixed-region PP-OCR text detection Action manifests.
package ocrregionsaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const maxManifestBytes = 32 << 10
const MaxRegionPixels = 262_144

type Config struct {
	SchemaVersion    uint32                  `json:"schemaVersion"`
	Title            string                  `json:"title"`
	InputSchema      string                  `json:"inputSchema"`
	OutputSchema     string                  `json:"outputSchema"`
	ReferenceRegion  capture.ReferenceRegion `json:"referenceRegion"`
	Sampling         capture.Sampling        `json:"sampling"`
	MaxPixels        uint64                  `json:"maxPixels"`
	LeftContextWidth uint32                  `json:"leftContextWidth"`
	VerticalPadding  uint32                  `json:"verticalPadding"`
}

func Load(root string) (Config, error) {
	if root == "" || !filepath.IsAbs(root) {
		return Config{}, errors.New("OCR text regions Action root must be absolute")
	}
	name := filepath.Join(root, "manifest.json")
	info, err := os.Stat(name)
	if err != nil {
		return Config{}, fmt.Errorf("stat OCR text regions Action manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return Config{}, fmt.Errorf("OCR text regions Action manifest must be a regular file at most %d bytes", maxManifestBytes)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Config{}, fmt.Errorf("read OCR text regions Action manifest: %w", err)
	}
	if err := strictjson.Validate(data); err != nil {
		return Config{}, fmt.Errorf("OCR text regions Action manifest must be strict JSON: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode OCR text regions Action manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("OCR text regions Action manifest contains multiple JSON values")
	}
	if err := config.Validate(root); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate(root string) error {
	if c.SchemaVersion != 1 {
		return errors.New("OCR text regions Action manifest schemaVersion must equal 1")
	}
	if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Title) != c.Title {
		return errors.New("OCR text regions Action title must be non-empty and canonical")
	}
	for label, name := range map[string]string{"inputSchema": c.InputSchema, "outputSchema": c.OutputSchema} {
		if filepath.Base(name) != name || filepath.Ext(name) != ".json" {
			return fmt.Errorf("OCR text regions Action %s must be one canonical JSON filename", label)
		}
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("OCR text regions Action %s must reference an existing regular file", label)
		}
	}
	if err := c.ReferenceRegion.Validate(); err != nil {
		return fmt.Errorf("OCR text regions Action referenceRegion: %w", err)
	}
	if c.Sampling != capture.SamplingReference && c.Sampling != capture.SamplingNative {
		return errors.New("OCR text regions Action sampling must equal reference or native")
	}
	if c.MaxPixels == 0 || c.MaxPixels > MaxRegionPixels {
		return fmt.Errorf("OCR text regions Action maxPixels must be from 1 through %d", MaxRegionPixels)
	}
	if c.Sampling == capture.SamplingReference &&
		uint64(c.ReferenceRegion.Width)*uint64(c.ReferenceRegion.Height) > c.MaxPixels {
		return errors.New("OCR text regions Action reference region exceeds maxPixels at reference density")
	}
	if c.LeftContextWidth == 0 || c.LeftContextWidth > 256 {
		return errors.New("OCR text regions Action leftContextWidth must be from 1 through 256 reference pixels")
	}
	if c.VerticalPadding > 64 {
		return errors.New("OCR text regions Action verticalPadding must be at most 64 reference pixels")
	}
	return nil
}
