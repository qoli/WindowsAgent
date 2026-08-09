package scriptpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.starlark.net/starlark"
)

const (
	ManifestName = "manifest.json"
	maxFileBytes = 4 << 20
)

type Manifest struct {
	SchemaVersion   uint32                   `json:"schemaVersion"`
	Version         uint32                   `json:"version"`
	Title           string                   `json:"title"`
	Entrypoint      string                   `json:"entrypoint"`
	TaskDocument    string                   `json:"taskDocument"`
	InputSchema     string                   `json:"inputSchema"`
	OutputSchema    string                   `json:"outputSchema"`
	Files           []string                 `json:"files"`
	Permissions     Permissions              `json:"permissions"`
	NativeLibraries map[string]NativeLibrary `json:"nativeLibraries,omitempty"`
	Limits          Limits                   `json:"limits"`
}

type Permissions struct {
	Memory *MemoryPermissions `json:"memory"`
	File   *FilePermissions   `json:"file"`
	Screen *ScreenPermissions `json:"screen"`
}

type ScreenPermissions struct {
	Operations []string `json:"operations"`
	MaxCalls   uint32   `json:"maxCalls"`
	MaxPixels  uint64   `json:"maxPixels"`
}

type MemoryPermissions struct {
	Target       string   `json:"target"`
	Operations   []string `json:"operations"`
	MaxCalls     uint32   `json:"maxCalls"`
	MaxBytesRead uint64   `json:"maxBytesRead"`
}

type FilePermissions struct {
	Roots        []FileRoot `json:"roots"`
	Operations   []string   `json:"operations"`
	MaxCalls     uint32     `json:"maxCalls"`
	MaxBytesRead uint64     `json:"maxBytesRead"`
}

type FileRoot struct {
	ID       string           `json:"id"`
	Resolver FileRootResolver `json:"resolver"`
}

type FileRootResolver struct {
	Kind        string `json:"kind"`
	KnownFolder string `json:"knownFolder"`
	Relative    string `json:"relative"`
}

type NativeLibrary struct {
	Platform             string `json:"platform"`
	Artifact             string `json:"artifact"`
	MaxCalls             uint32 `json:"maxCalls"`
	MaxNativeMemoryBytes uint64 `json:"maxNativeMemoryBytes"`
}

type Limits struct {
	WallTimeMS     uint64 `json:"wallTimeMs"`
	MaxSteps       uint64 `json:"maxSteps"`
	MaxResultBytes uint64 `json:"maxResultBytes"`
	MaxLogBytes    uint64 `json:"maxLogBytes"`
}

type Identity struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
}

type Package struct {
	Root           string
	Manifest       Manifest
	Identity       Identity
	Script         []byte
	Task           []byte
	InputSchema    []byte
	OutputSchema   []byte
	compiledInput  *jsonschema.Schema
	compiledSchema *jsonschema.Schema
}

func Load(root, capabilityID string) (*Package, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("script package root must be absolute")
	}
	if capabilityID == "" || strings.TrimSpace(capabilityID) != capabilityID {
		return nil, errors.New("script capability ID is required and must be canonical")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve script package root: %w", err)
	}
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("stat script package root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("script package root must be a directory")
	}
	manifestBytes, err := readBoundedFile(filepath.Join(canonicalRoot, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}
	expectedNames := map[string]struct{}{ManifestName: {}}
	contents := make(map[string][]byte, len(manifest.Files))
	for _, name := range manifest.Files {
		expectedNames[name] = struct{}{}
		content, err := readPackageFile(canonicalRoot, name)
		if err != nil {
			return nil, fmt.Errorf("read package file %q: %w", name, err)
		}
		contents[name] = content
	}
	if err := rejectUndeclaredFiles(canonicalRoot, expectedNames); err != nil {
		return nil, err
	}
	script := contents[manifest.Entrypoint]
	_, program, err := starlark.SourceProgram(
		manifest.Entrypoint,
		script,
		func(name string) bool {
			return name == "observer" || name == "native" || name == "job" || name == "math"
		},
	)
	if err != nil {
		return nil, fmt.Errorf("compile Starlark entrypoint: %w", err)
	}
	if program.NumLoads() != 0 {
		return nil, errors.New("Starlark load statements are forbidden")
	}
	task := contents[manifest.TaskDocument]
	if len(bytes.TrimSpace(task)) == 0 {
		return nil, errors.New("TASK.md must not be empty")
	}
	inputSchemaBytes := contents[manifest.InputSchema]
	compiledInput, err := compileSchema(inputSchemaBytes, "input.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile input schema: %w", err)
	}
	schemaBytes := contents[manifest.OutputSchema]
	compiledSchema, err := compileSchema(schemaBytes, "output.schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile output schema: %w", err)
	}
	return &Package{
		Root:     canonicalRoot,
		Manifest: manifest,
		Identity: Identity{
			ID:      capabilityID,
			Version: manifest.Version,
		},
		Script:         script,
		Task:           task,
		InputSchema:    inputSchemaBytes,
		OutputSchema:   schemaBytes,
		compiledInput:  compiledInput,
		compiledSchema: compiledSchema,
	}, nil
}

func (p *Package) ValidateInputs(value any) error {
	if p == nil || p.compiledInput == nil {
		return errors.New("compiled input schema is required")
	}
	if err := p.compiledInput.Validate(value); err != nil {
		return fmt.Errorf("input schema validation failed: %w", err)
	}
	return nil
}

func (p *Package) ValidateOutput(value any) error {
	if p == nil || p.compiledSchema == nil {
		return errors.New("compiled output schema is required")
	}
	if err := p.compiledSchema.Validate(value); err != nil {
		return fmt.Errorf("output schema validation failed: %w", err)
	}
	return nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 2 {
		return fmt.Errorf("schemaVersion must equal 2, got %d", manifest.SchemaVersion)
	}
	if manifest.Version == 0 {
		return errors.New("version must be positive")
	}
	if manifest.Title == "" {
		return errors.New("title is required")
	}
	declaredFiles := make(map[string]struct{}, len(manifest.Files))
	for _, name := range manifest.Files {
		if err := validateRelativeName(name); err != nil {
			return err
		}
		if _, duplicate := declaredFiles[name]; duplicate {
			return fmt.Errorf("duplicate file declaration %q", name)
		}
		declaredFiles[name] = struct{}{}
	}
	requiredFiles := []string{
		manifest.Entrypoint,
		manifest.TaskDocument,
		manifest.InputSchema,
		manifest.OutputSchema,
	}
	seen := make(map[string]struct{}, len(requiredFiles))
	for _, name := range requiredFiles {
		if err := validateRelativeName(name); err != nil {
			return err
		}
		if _, duplicate := seen[name]; duplicate {
			return errors.New("entrypoint, taskDocument, inputSchema, and outputSchema must be distinct")
		}
		seen[name] = struct{}{}
		if !containsFile(manifest.Files, name) {
			return fmt.Errorf("files is missing %q", name)
		}
	}
	if filepath.Ext(manifest.Entrypoint) != ".star" {
		return errors.New("entrypoint must be a .star file")
	}
	if manifest.TaskDocument != "TASK.md" {
		return errors.New("taskDocument must equal TASK.md in V2")
	}
	if filepath.Ext(manifest.InputSchema) != ".json" {
		return errors.New("inputSchema must be a JSON file")
	}
	if filepath.Ext(manifest.OutputSchema) != ".json" {
		return errors.New("outputSchema must be a JSON file")
	}
	if memory := manifest.Permissions.Memory; memory != nil {
		if memory.Target == "" || memory.MaxCalls == 0 || memory.MaxBytesRead == 0 {
			return errors.New("memory target, maxCalls, and maxBytesRead are required")
		}
		if memory.Target != "rule/current-process" {
			return fmt.Errorf("unsupported memory target %q", memory.Target)
		}
		if err := validateOperations("memory", memory.Operations, []string{"modules", "regions", "scan", "resolveRip", "readBatch", "readStrided"}); err != nil {
			return err
		}
	}
	if file := manifest.Permissions.File; file != nil {
		if len(file.Roots) == 0 || file.MaxCalls == 0 || file.MaxBytesRead == 0 {
			return errors.New("file roots, maxCalls, and maxBytesRead are required")
		}
		seenRoots := make(map[string]struct{}, len(file.Roots))
		for _, root := range file.Roots {
			if !resourceAliasPattern.MatchString(root.ID) {
				return fmt.Errorf("file root alias %q is not canonical", root.ID)
			}
			if _, duplicate := seenRoots[root.ID]; duplicate {
				return fmt.Errorf("duplicate file root alias %q", root.ID)
			}
			seenRoots[root.ID] = struct{}{}
			if root.Resolver.Kind != "windows-known-folder" {
				return fmt.Errorf("unsupported file root resolver %q", root.Resolver.Kind)
			}
			if root.Resolver.KnownFolder != "LocalAppData" && root.Resolver.KnownFolder != "SavedGames" {
				return fmt.Errorf("unsupported Windows known folder %q", root.Resolver.KnownFolder)
			}
			if err := validateRelativeName(root.Resolver.Relative); err != nil {
				return fmt.Errorf("file root %q resolver path: %w", root.ID, err)
			}
		}
		if err := validateOperations("file", file.Operations, []string{"list", "stat", "read", "readJson", "hash", "openBlob"}); err != nil {
			return err
		}
	}
	if screen := manifest.Permissions.Screen; screen != nil {
		if screen.MaxCalls == 0 || screen.MaxCalls > 16 || screen.MaxPixels == 0 || screen.MaxPixels > 65_536 {
			return errors.New("screen maxCalls and maxPixels must be from 1 through 16 and 1 through 65536 respectively")
		}
		if err := validateOperations("screen", screen.Operations, []string{"readRegion"}); err != nil {
			return err
		}
	}
	if len(manifest.NativeLibraries) > 4 {
		return errors.New("at most four native libraries may be declared")
	}
	for alias, library := range manifest.NativeLibraries {
		if !nativeAliasPattern.MatchString(alias) {
			return fmt.Errorf("native library alias %q is not canonical", alias)
		}
		if !nativePlatformPattern.MatchString(library.Platform) {
			return fmt.Errorf("native library %q platform is not canonical", alias)
		}
		if err := validateRelativeName(library.Artifact); err != nil {
			return fmt.Errorf("native library %q artifact: %w", alias, err)
		}
		if filepath.Ext(library.Artifact) != ".dll" {
			return fmt.Errorf("native library %q artifact must be a DLL", alias)
		}
		if !containsFile(manifest.Files, library.Artifact) {
			return fmt.Errorf("native library %q artifact must be declared in files", alias)
		}
		if library.MaxCalls == 0 || library.MaxCalls > 1024 ||
			library.MaxNativeMemoryBytes == 0 || library.MaxNativeMemoryBytes > 1<<30 {
			return fmt.Errorf("native library %q call or memory limit is invalid", alias)
		}
	}
	if manifest.Limits.WallTimeMS == 0 ||
		manifest.Limits.WallTimeMS > 60_000 ||
		manifest.Limits.MaxSteps == 0 ||
		manifest.Limits.MaxResultBytes == 0 ||
		manifest.Limits.MaxLogBytes == 0 {
		return errors.New("script limits must be positive and wallTimeMs must not exceed 60000")
	}
	return nil
}

func containsFile(files []string, name string) bool {
	for _, candidate := range files {
		if candidate == name {
			return true
		}
	}
	return false
}

var nativeAliasPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var nativePlatformPattern = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
var resourceAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func validateOperations(namespace string, operations, allowed []string) error {
	if len(operations) == 0 {
		return fmt.Errorf("%s operations must not be empty", namespace)
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if _, ok := seen[operation]; ok {
			return fmt.Errorf("duplicate %s operation %q", namespace, operation)
		}
		seen[operation] = struct{}{}
		found := false
		for _, candidate := range allowed {
			if operation == candidate {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unsupported %s operation %q", namespace, operation)
		}
	}
	return nil
}

func validateRelativeName(name string) error {
	if name == "" || filepath.IsAbs(name) || path.Clean(name) != name ||
		strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return fmt.Errorf("package path %q is not canonical and relative", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("package path %q escapes the package", name)
	}
	return nil
}

func readPackageFile(root, name string) ([]byte, error) {
	if err := validateRelativeName(name); err != nil {
		return nil, err
	}
	fullPath := filepath.Join(root, filepath.FromSlash(name))
	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("package symlinks are forbidden")
	}
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return nil, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("package file resolves outside package root")
	}
	return readBoundedFile(resolved)
}

func readBoundedFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("package member must be a regular file")
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("package member exceeds %d bytes", maxFileBytes)
	}
	return os.ReadFile(path)
}

func rejectUndeclaredFiles(root string, expected map[string]struct{}) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if entry.IsDir() {
			return nil
		}
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("undeclared package file %q", name)
		}
		return nil
	})
}

func decodeStrictJSON(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return fmt.Errorf("decode trailing JSON content: %w", err)
	}
	return nil
}

type denyingLoader struct{}

func (denyingLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource is forbidden: %s", url)
}

func compileSchema(data []byte, name string) (*jsonschema.Schema, error) {
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
	schemaURL := "https://windowsagent.invalid/" + name
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaURL)
}
