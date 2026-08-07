package streamaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ManifestName      = "manifest.json"
	maxFileBytes      = 4 << 20
	eventPayloadLimit = 256 << 10
)

type Manifest struct {
	SchemaVersion uint32   `json:"schemaVersion"`
	Version       uint32   `json:"version"`
	Title         string   `json:"title"`
	Entrypoint    string   `json:"entrypoint"`
	TaskDocument  string   `json:"taskDocument"`
	InputSchema   string   `json:"inputSchema"`
	OutputSchema  string   `json:"outputSchema"`
	EventSchema   string   `json:"eventSchema"`
	Files         []string `json:"files"`
	Limits        Limits   `json:"limits"`
}

type Limits struct {
	MaxSteps       uint64 `json:"maxSteps"`
	MaxOutputBytes uint64 `json:"maxOutputBytes"`
	MaxEventBytes  uint64 `json:"maxEventBytes"`
	MaxSleepMs     uint64 `json:"maxSleepMs"`
}

type Package struct {
	Root           string
	Manifest       Manifest
	Script         []byte
	InputSchema    []byte
	OutputSchema   []byte
	EventSchema    []byte
	compiledInput  *jsonschema.Schema
	compiledOutput *jsonschema.Schema
	compiledEvent  *jsonschema.Schema
}

func Load(root string) (*Package, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("streaming Action package root must be absolute")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve streaming Action package root: %w", err)
	}
	manifestBytes, err := readMember(filepath.Join(canonicalRoot, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("read streaming Action manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrict(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode streaming Action manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	if err := validateMembers(canonicalRoot, manifest.Files); err != nil {
		return nil, err
	}
	script, err := readMember(filepath.Join(canonicalRoot, filepath.FromSlash(manifest.Entrypoint)))
	if err != nil {
		return nil, err
	}
	inputSchema, err := readMember(filepath.Join(canonicalRoot, filepath.FromSlash(manifest.InputSchema)))
	if err != nil {
		return nil, err
	}
	outputSchema, err := readMember(filepath.Join(canonicalRoot, filepath.FromSlash(manifest.OutputSchema)))
	if err != nil {
		return nil, err
	}
	eventSchema, err := readMember(filepath.Join(canonicalRoot, filepath.FromSlash(manifest.EventSchema)))
	if err != nil {
		return nil, err
	}
	compiledInput, err := compileSchema("input.schema.json", inputSchema)
	if err != nil {
		return nil, fmt.Errorf("compile streaming Action input schema: %w", err)
	}
	compiledOutput, err := compileSchema("output.schema.json", outputSchema)
	if err != nil {
		return nil, fmt.Errorf("compile streaming Action output schema: %w", err)
	}
	compiledEvent, err := compileSchema("event.schema.json", eventSchema)
	if err != nil {
		return nil, fmt.Errorf("compile streaming Action event schema: %w", err)
	}
	return &Package{
		Root: canonicalRoot, Manifest: manifest, Script: script,
		InputSchema: inputSchema, OutputSchema: outputSchema, EventSchema: eventSchema,
		compiledInput: compiledInput, compiledOutput: compiledOutput, compiledEvent: compiledEvent,
	}, nil
}

func (p *Package) ValidateInput(value any) error {
	if p == nil || p.compiledInput == nil {
		return errors.New("streaming Action package is required")
	}
	return p.compiledInput.Validate(value)
}

func (p *Package) ValidateOutput(value any) error {
	if p == nil || p.compiledOutput == nil {
		return errors.New("streaming Action package is required")
	}
	return p.compiledOutput.Validate(value)
}

func (p *Package) ValidateEvent(value any) error {
	if p == nil || p.compiledEvent == nil {
		return errors.New("streaming Action package is required")
	}
	return p.compiledEvent.Validate(value)
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 1 || manifest.Version == 0 {
		return errors.New("streaming Action manifest schemaVersion must equal 1 and version must be positive")
	}
	if strings.TrimSpace(manifest.Title) == "" || strings.TrimSpace(manifest.Title) != manifest.Title {
		return errors.New("streaming Action title must be non-empty and canonical")
	}
	for label, name := range map[string]string{
		"entrypoint": manifest.Entrypoint, "taskDocument": manifest.TaskDocument,
		"inputSchema": manifest.InputSchema, "outputSchema": manifest.OutputSchema, "eventSchema": manifest.EventSchema,
	} {
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "..") {
			return fmt.Errorf("streaming Action %s must be one canonical relative file", label)
		}
	}
	if filepath.Ext(manifest.Entrypoint) != ".star" || filepath.Ext(manifest.TaskDocument) != ".md" ||
		filepath.Ext(manifest.InputSchema) != ".json" || filepath.Ext(manifest.OutputSchema) != ".json" || filepath.Ext(manifest.EventSchema) != ".json" {
		return errors.New("streaming Action manifest file extensions are invalid")
	}
	if manifest.Files == nil || len(manifest.Files) < 5 {
		return errors.New("streaming Action files must declare every package member")
	}
	if manifest.Limits.MaxSteps == 0 || manifest.Limits.MaxOutputBytes == 0 || manifest.Limits.MaxEventBytes == 0 || manifest.Limits.MaxSleepMs == 0 {
		return errors.New("streaming Action limits must be positive")
	}
	if manifest.Limits.MaxOutputBytes > 1<<20 || manifest.Limits.MaxEventBytes > eventPayloadLimit || manifest.Limits.MaxSleepMs > 60_000 {
		return errors.New("streaming Action output, event, or sleep limit exceeds Host maximum")
	}
	return nil
}

func validateMembers(root string, declared []string) error {
	want := append([]string(nil), declared...)
	sort.Strings(want)
	for index, name := range want {
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "..") {
			return fmt.Errorf("streaming Action member %q is not canonical", name)
		}
		if index != 0 && name == want[index-1] {
			return fmt.Errorf("streaming Action member %q is declared more than once", name)
		}
	}
	var found []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("streaming Action symlink is forbidden: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.ToSlash(relative) == ManifestName {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("streaming Action member is not regular: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("streaming Action member exceeds %d bytes: %s", maxFileBytes, relative)
		}
		found = append(found, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(found)
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("streaming Action package members do not match manifest: found=%v declared=%v", found, want)
	}
	return nil
}

func readMember(name string) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, errors.New("streaming Action member must be one bounded regular file")
	}
	return os.ReadFile(name)
}

func compileSchema(name string, data []byte) (*jsonschema.Schema, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyingLoader{})
	schemaURL := "https://windowsagent.invalid/streaming/" + name
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaURL)
}

type denyingLoader struct{}

func (denyingLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource is forbidden: %s", url)
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
