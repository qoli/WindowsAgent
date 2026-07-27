package scriptpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	SchemaVersion uint32                  `json:"schemaVersion"`
	ID            string                  `json:"id"`
	Version       uint32                  `json:"version"`
	Title         string                  `json:"title"`
	Entrypoint    string                  `json:"entrypoint"`
	TaskDocument  string                  `json:"taskDocument"`
	OutputSchema  string                  `json:"outputSchema"`
	Files         map[string]FileIdentity `json:"files"`
	Permissions   Permissions             `json:"permissions"`
	Limits        Limits                  `json:"limits"`
}

type FileIdentity struct {
	SHA256 string `json:"sha256"`
}

type Permissions struct {
	Memory *MemoryPermissions `json:"memory"`
	File   *FilePermissions   `json:"file"`
}

type MemoryPermissions struct {
	Target       string   `json:"target"`
	Operations   []string `json:"operations"`
	MaxCalls     uint32   `json:"maxCalls"`
	MaxBytesRead uint64   `json:"maxBytesRead"`
}

type FilePermissions struct {
	Roots        []string `json:"roots"`
	Operations   []string `json:"operations"`
	MaxCalls     uint32   `json:"maxCalls"`
	MaxBytesRead uint64   `json:"maxBytesRead"`
}

type Limits struct {
	WallTimeMS     uint64 `json:"wallTimeMs"`
	MaxSteps       uint64 `json:"maxSteps"`
	MaxResultBytes uint64 `json:"maxResultBytes"`
	MaxLogBytes    uint64 `json:"maxLogBytes"`
}

type Identity struct {
	ID             string `json:"id"`
	Version        uint32 `json:"version"`
	ManifestSHA256 string `json:"manifestSha256"`
	PackageSHA256  string `json:"packageSha256"`
}

type Package struct {
	Root           string
	Manifest       Manifest
	Identity       Identity
	Script         []byte
	Task           []byte
	OutputSchema   []byte
	compiledSchema *jsonschema.Schema
}

func Load(root string) (*Package, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("script package root must be absolute")
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
	for name, identity := range manifest.Files {
		expectedNames[name] = struct{}{}
		content, err := readPackageFile(canonicalRoot, name)
		if err != nil {
			return nil, fmt.Errorf("read package file %q: %w", name, err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != identity.SHA256 {
			return nil, fmt.Errorf("package file %q SHA-256 mismatch", name)
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
			return name == "observer" || name == "job"
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
	schemaBytes := contents[manifest.OutputSchema]
	compiledSchema, err := compileSchema(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("compile output schema: %w", err)
	}
	manifestDigest := sha256.Sum256(manifestBytes)
	packageDigest := computePackageDigest(manifestDigest, manifest.Files)
	return &Package{
		Root:     canonicalRoot,
		Manifest: manifest,
		Identity: Identity{
			ID:             manifest.ID,
			Version:        manifest.Version,
			ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
			PackageSHA256:  hex.EncodeToString(packageDigest[:]),
		},
		Script:         script,
		Task:           task,
		OutputSchema:   schemaBytes,
		compiledSchema: compiledSchema,
	}, nil
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
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must equal 1, got %d", manifest.SchemaVersion)
	}
	if manifest.ID == "" || strings.TrimSpace(manifest.ID) != manifest.ID {
		return errors.New("id is required and must be canonical")
	}
	if manifest.Version == 0 {
		return errors.New("version must be positive")
	}
	if manifest.Title == "" {
		return errors.New("title is required")
	}
	requiredFiles := []string{manifest.Entrypoint, manifest.TaskDocument, manifest.OutputSchema}
	if len(manifest.Files) != len(requiredFiles) {
		return errors.New("files must contain exactly entrypoint, taskDocument, and outputSchema")
	}
	seen := make(map[string]struct{}, len(requiredFiles))
	for _, name := range requiredFiles {
		if err := validateRelativeName(name); err != nil {
			return err
		}
		if _, duplicate := seen[name]; duplicate {
			return errors.New("entrypoint, taskDocument, and outputSchema must be distinct")
		}
		seen[name] = struct{}{}
		identity, ok := manifest.Files[name]
		if !ok {
			return fmt.Errorf("files is missing %q", name)
		}
		if err := validateSHA256(identity.SHA256); err != nil {
			return fmt.Errorf("invalid SHA-256 for %q: %w", name, err)
		}
	}
	if filepath.Ext(manifest.Entrypoint) != ".star" {
		return errors.New("entrypoint must be a .star file")
	}
	if manifest.TaskDocument != "TASK.md" {
		return errors.New("taskDocument must equal TASK.md in V1")
	}
	if filepath.Ext(manifest.OutputSchema) != ".json" {
		return errors.New("outputSchema must be a JSON file")
	}
	if manifest.Permissions.Memory == nil && manifest.Permissions.File == nil {
		return errors.New("at least one observer permission is required")
	}
	if memory := manifest.Permissions.Memory; memory != nil {
		if memory.Target == "" || memory.MaxCalls == 0 || memory.MaxBytesRead == 0 {
			return errors.New("memory target, maxCalls, and maxBytesRead are required")
		}
		if err := validateOperations("memory", memory.Operations, []string{"modules", "regions", "scan", "resolveRip", "readBatch", "readStrided"}); err != nil {
			return err
		}
	}
	if file := manifest.Permissions.File; file != nil {
		if len(file.Roots) == 0 || file.MaxCalls == 0 || file.MaxBytesRead == 0 {
			return errors.New("file roots, maxCalls, and maxBytesRead are required")
		}
		if err := validateOperations("file", file.Operations, []string{"stat", "read", "hash", "decode"}); err != nil {
			return err
		}
	}
	if manifest.Limits.WallTimeMS == 0 ||
		manifest.Limits.MaxSteps == 0 ||
		manifest.Limits.MaxResultBytes == 0 ||
		manifest.Limits.MaxLogBytes == 0 {
		return errors.New("all script limits must be positive")
	}
	return nil
}

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
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || strings.Contains(name, `\`) {
		return fmt.Errorf("package path %q is not canonical and relative", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("package path %q escapes the package", name)
	}
	return nil
}

func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return errors.New("digest must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return err
	}
	return nil
}

func readPackageFile(root, name string) ([]byte, error) {
	if err := validateRelativeName(name); err != nil {
		return nil, err
	}
	fullPath := filepath.Join(root, filepath.FromSlash(name))
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
			return fmt.Errorf("package directories are unsupported in V1: %q", name)
		}
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("undeclared package file %q", name)
		}
		return nil
	})
}

func computePackageDigest(manifestDigest [sha256.Size]byte, files map[string]FileIdentity) [sha256.Size]byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	hash.Write([]byte("windowsagent-observation-package-v1\x00"))
	hash.Write(manifestDigest[:])
	for _, name := range names {
		hash.Write([]byte{0})
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		digest, _ := hex.DecodeString(files[name].SHA256)
		hash.Write(digest)
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
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

func compileSchema(data []byte) (*jsonschema.Schema, error) {
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
	const schemaURL = "https://windowsagent.invalid/output.schema.json"
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, err
	}
	return compiler.Compile(schemaURL)
}
