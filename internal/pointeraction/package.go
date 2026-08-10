package pointeraction

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Manifest struct {
	SchemaVersion uint32 `json:"schemaVersion"`
	Version       uint32 `json:"version"`
	Title         string `json:"title"`
	InputSchema   string `json:"inputSchema"`
	OutputSchema  string `json:"outputSchema"`
	TaskDocument  string `json:"taskDocument"`
}

type Package struct {
	Manifest     Manifest
	InputSchema  []byte
	OutputSchema []byte
	input        *jsonschema.Schema
	output       *jsonschema.Schema
}

func Load(root string) (*Package, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("pointer Action package root must be absolute")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if err := strictjson.Validate(manifestBytes); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(manifestBytes))
	dec.DisallowUnknownFields()
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != 1 || manifest.Version == 0 || manifest.Title == "" || manifest.InputSchema != "input.schema.json" || manifest.OutputSchema != "output.schema.json" || manifest.TaskDocument != "TASK.md" {
		return nil, errors.New("pointer Action manifest is invalid")
	}
	inputBytes, err := os.ReadFile(filepath.Join(root, manifest.InputSchema))
	if err != nil {
		return nil, err
	}
	outputBytes, err := os.ReadFile(filepath.Join(root, manifest.OutputSchema))
	if err != nil {
		return nil, err
	}
	input, err := compile("input", inputBytes)
	if err != nil {
		return nil, err
	}
	output, err := compile("output", outputBytes)
	if err != nil {
		return nil, err
	}
	return &Package{Manifest: manifest, InputSchema: inputBytes, OutputSchema: outputBytes, input: input, output: output}, nil
}

func compile(name string, data []byte) (*jsonschema.Schema, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	url := "https://windowsagent.invalid/pointer/" + name
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
}

func (p *Package) ValidateInput(v any) error {
	if p == nil || p.input == nil {
		return errors.New("pointer Action package is required")
	}
	return p.input.Validate(v)
}
func (p *Package) ValidateOutput(v any) error {
	if p == nil || p.output == nil {
		return errors.New("pointer Action package is required")
	}
	return p.output.Validate(v)
}
