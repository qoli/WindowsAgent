// Package inputaction loads and executes finite, Rule-owned keyboard Actions.
package inputaction

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
	ManifestName = "manifest.json"
	maxFileBytes = 1 << 20
)

type Manifest struct {
	SchemaVersion uint32             `json:"schemaVersion"`
	Version       uint32             `json:"version"`
	Title         string             `json:"title"`
	InputSchema   string             `json:"inputSchema"`
	OutputSchema  string             `json:"outputSchema"`
	TaskDocument  string             `json:"taskDocument"`
	Selector      Selector           `json:"selector"`
	Bindings      map[string]Binding `json:"bindings"`
	Files         []string           `json:"files"`
}

type Selector struct {
	Constant   string `json:"constant,omitempty"`
	InputField string `json:"inputField,omitempty"`
}

type Binding struct {
	Control string `json:"control"`
}

type Package struct {
	Root           string
	Manifest       Manifest
	InputSchema    []byte
	OutputSchema   []byte
	compiledInput  *jsonschema.Schema
	compiledOutput *jsonschema.Schema
}

func Load(root string) (*Package, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("input Action package root must be absolute")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve input Action package root: %w", err)
	}
	manifestBytes, err := readMember(filepath.Join(canonicalRoot, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("read input Action manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrict(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode input Action manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	if err := validateMembers(canonicalRoot, manifest.Files); err != nil {
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
	compiledInput, err := compileSchema("input.schema.json", inputSchema)
	if err != nil {
		return nil, fmt.Errorf("compile input Action input schema: %w", err)
	}
	compiledOutput, err := compileSchema("output.schema.json", outputSchema)
	if err != nil {
		return nil, fmt.Errorf("compile input Action output schema: %w", err)
	}
	return &Package{
		Root: canonicalRoot, Manifest: manifest, InputSchema: inputSchema, OutputSchema: outputSchema,
		compiledInput: compiledInput, compiledOutput: compiledOutput,
	}, nil
}

func (p *Package) ValidateInput(value any) error {
	if p == nil || p.compiledInput == nil {
		return errors.New("input Action package is required")
	}
	return p.compiledInput.Validate(value)
}

func (p *Package) ValidateOutput(value any) error {
	if p == nil || p.compiledOutput == nil {
		return errors.New("input Action package is required")
	}
	return p.compiledOutput.Validate(value)
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 1 || manifest.Version == 0 {
		return errors.New("input Action manifest schemaVersion must equal 1 and version must be positive")
	}
	if strings.TrimSpace(manifest.Title) == "" || strings.TrimSpace(manifest.Title) != manifest.Title {
		return errors.New("input Action title must be non-empty and canonical")
	}
	for label, name := range map[string]string{
		"inputSchema": manifest.InputSchema, "outputSchema": manifest.OutputSchema, "taskDocument": manifest.TaskDocument,
	} {
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "..") {
			return fmt.Errorf("input Action %s must be one canonical relative file", label)
		}
	}
	constant := manifest.Selector.Constant != ""
	field := manifest.Selector.InputField != ""
	if constant == field {
		return errors.New("input Action selector must declare exactly one of constant or inputField")
	}
	if field && strings.TrimSpace(manifest.Selector.InputField) != manifest.Selector.InputField {
		return errors.New("input Action selector inputField must be canonical")
	}
	if len(manifest.Bindings) == 0 {
		return errors.New("input Action bindings must not be empty")
	}
	if constant {
		if _, ok := manifest.Bindings[manifest.Selector.Constant]; !ok {
			return errors.New("input Action selector constant does not name a binding")
		}
	}
	for name, binding := range manifest.Bindings {
		if name == "" || strings.TrimSpace(name) != name || binding.Control == "" || strings.TrimSpace(binding.Control) != binding.Control {
			return fmt.Errorf("input Action binding %q is not canonical", name)
		}
	}
	if manifest.Files == nil || len(manifest.Files) != 3 {
		return errors.New("input Action files must declare exactly task, input schema, and output schema")
	}
	return nil
}

func validateMembers(root string, declared []string) error {
	want := append([]string(nil), declared...)
	sort.Strings(want)
	for index, name := range want {
		if name == "" || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || strings.Contains(name, "..") {
			return fmt.Errorf("input Action member %q is not canonical", name)
		}
		if index != 0 && name == want[index-1] {
			return fmt.Errorf("input Action member %q is declared more than once", name)
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
			return fmt.Errorf("input Action symlink is forbidden: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.ToSlash(relative) == ManifestName {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("input Action member is not regular: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("input Action member is too large: %s", relative)
		}
		found = append(found, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(found)
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("input Action package members do not match manifest: found=%v declared=%v", found, want)
	}
	return nil
}

func readMember(name string) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, errors.New("input Action member must be one bounded regular file")
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
	url := "https://windowsagent.invalid/input-action/" + name
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

type denyingLoader struct{}

func (denyingLoader) Load(string) (any, error) {
	return nil, errors.New("external schema resources are forbidden")
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
