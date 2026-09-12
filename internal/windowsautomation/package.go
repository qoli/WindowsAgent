// Package windowsautomation executes host-owned ephemeral Starlark Actions.
package windowsautomation

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
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
	"time"

	"github.com/qoli/WindowsAgent/internal/strictjson"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

const (
	RuntimeID         = "windows-starlark-action-v1"
	ManifestName      = "manifest.json"
	ActivityEventType = "action.activity"
)

type Limits struct {
	WallTimeMS            uint64 `json:"wallTimeMs"`
	MaxSteps              uint64 `json:"maxSteps"`
	MaxResultBytes        uint64 `json:"maxResultBytes"`
	MaxProcessOutputBytes uint64 `json:"maxProcessOutputBytes"`
}

type Manifest struct {
	SchemaVersion uint32   `json:"schemaVersion"`
	Version       uint32   `json:"version"`
	Title         string   `json:"title"`
	Entrypoint    string   `json:"entrypoint"`
	TaskDocument  string   `json:"taskDocument"`
	InputSchema   string   `json:"inputSchema"`
	OutputSchema  string   `json:"outputSchema"`
	Files         []string `json:"files"`
	Limits        Limits   `json:"limits"`
}

type Package struct {
	Manifest     Manifest
	Script       []byte
	Task         []byte
	InputSchema  []byte
	OutputSchema []byte
	Digest       string

	compiledInput  *jsonschema.Schema
	compiledOutput *jsonschema.Schema
}

func LoadDirectory(root string) (*Package, error) {
	members, err := readDirectoryMembers(root)
	if err != nil {
		return nil, err
	}
	return loadMembers(members)
}

// ArchiveDirectory validates a package and returns its canonical ZIP form.
func ArchiveDirectory(root string) ([]byte, error) {
	members, err := readDirectoryMembers(root)
	if err != nil {
		return nil, err
	}
	if _, err := loadMembers(members); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o600)
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		member, err := archive.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create ZIP member %s: %w", name, err)
		}
		if _, err := member.Write(members[name]); err != nil {
			return nil, fmt.Errorf("write ZIP member %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finish package ZIP: %w", err)
	}
	return output.Bytes(), nil
}

func readDirectoryMembers(root string) (map[string][]byte, error) {
	if root == "" {
		return nil, errors.New("package directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve package directory: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve package directory links: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("stat package directory: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("package path must be a directory")
	}
	members := map[string][]byte{}
	err = filepath.WalkDir(canonical, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(canonical, name)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package symlink is forbidden: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("package member must be regular: %s", relative)
		}
		memberName := filepath.ToSlash(relative)
		if err := validateMemberName(memberName); err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		members[memberName] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read package directory: %w", err)
	}
	return members, nil
}

// LoadArchive loads a deterministic ZIP representation. Members must be
// regular files in lexical order; their archive metadata does not affect the
// semantic package digest.
func LoadArchive(data []byte) (*Package, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open package ZIP: %w", err)
	}
	members := map[string][]byte{}
	previous := ""
	for _, member := range reader.File {
		if err := validateMemberName(member.Name); err != nil {
			return nil, err
		}
		if member.FileInfo().IsDir() || !member.Mode().IsRegular() {
			return nil, fmt.Errorf("ZIP member must be a regular file: %s", member.Name)
		}
		if previous != "" && member.Name <= previous {
			return nil, errors.New("ZIP members must be unique and lexically ordered")
		}
		previous = member.Name
		stream, err := member.Open()
		if err != nil {
			return nil, fmt.Errorf("open ZIP member %s: %w", member.Name, err)
		}
		content, readErr := io.ReadAll(stream)
		closeErr := stream.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read ZIP member %s: %w", member.Name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close ZIP member %s: %w", member.Name, closeErr)
		}
		members[member.Name] = content
	}
	return loadMembers(members)
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
	if p == nil || p.compiledOutput == nil {
		return errors.New("compiled output schema is required")
	}
	if err := p.compiledOutput.Validate(value); err != nil {
		return fmt.Errorf("output schema validation failed: %w", err)
	}
	return nil
}

func loadMembers(members map[string][]byte) (*Package, error) {
	manifestBytes, ok := members[ManifestName]
	if !ok {
		return nil, errors.New("package is missing manifest.json")
	}
	var manifest Manifest
	if err := decodeStrict(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}
	want := append([]string{ManifestName}, manifest.Files...)
	sort.Strings(want)
	found := make([]string, 0, len(members))
	for name := range members {
		found = append(found, name)
	}
	sort.Strings(found)
	if strings.Join(want, "\n") != strings.Join(found, "\n") {
		return nil, fmt.Errorf("package members do not match manifest: found=%v declared=%v", found, want)
	}
	for _, name := range manifest.Files {
		if _, ok := members[name]; !ok {
			return nil, fmt.Errorf("package is missing declared member %q", name)
		}
	}
	if len(bytes.TrimSpace(members[manifest.TaskDocument])) == 0 {
		return nil, errors.New("TASK.md must not be empty")
	}
	input, err := compileSchema("input", members[manifest.InputSchema])
	if err != nil {
		return nil, fmt.Errorf("compile input schema: %w", err)
	}
	output, err := compileSchema("output", members[manifest.OutputSchema])
	if err != nil {
		return nil, fmt.Errorf("compile output schema: %w", err)
	}
	if err := validateProgram(manifest.Entrypoint, members[manifest.Entrypoint]); err != nil {
		return nil, err
	}
	return &Package{
		Manifest: manifest, Script: cloneBytes(members[manifest.Entrypoint]),
		Task: cloneBytes(members[manifest.TaskDocument]), InputSchema: cloneBytes(members[manifest.InputSchema]),
		OutputSchema: cloneBytes(members[manifest.OutputSchema]), Digest: digestMembers(members),
		compiledInput: input, compiledOutput: output,
	}, nil
}

func validateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must equal 1, got %d", manifest.SchemaVersion)
	}
	if manifest.Version == 0 {
		return errors.New("version must be positive")
	}
	if manifest.Title == "" || strings.TrimSpace(manifest.Title) != manifest.Title {
		return errors.New("title must be non-empty and canonical")
	}
	if manifest.Entrypoint != "main.star" || manifest.TaskDocument != "TASK.md" {
		return errors.New("entrypoint must equal main.star and taskDocument must equal TASK.md")
	}
	if filepath.Ext(manifest.InputSchema) != ".json" || filepath.Ext(manifest.OutputSchema) != ".json" {
		return errors.New("inputSchema and outputSchema must name JSON files")
	}
	required := []string{manifest.Entrypoint, manifest.TaskDocument, manifest.InputSchema, manifest.OutputSchema}
	if len(manifest.Files) != len(required) {
		return errors.New("files must declare exactly main.star, TASK.md, input schema, and output schema")
	}
	requiredSeen := map[string]bool{}
	for _, name := range required {
		if requiredSeen[name] {
			return errors.New("entrypoint, taskDocument, inputSchema, and outputSchema must be distinct")
		}
		requiredSeen[name] = true
	}
	seen := map[string]bool{}
	for _, name := range manifest.Files {
		if err := validateMemberName(name); err != nil {
			return err
		}
		if name == ManifestName {
			return errors.New("files must not include manifest.json")
		}
		if seen[name] {
			return fmt.Errorf("duplicate file declaration %q", name)
		}
		seen[name] = true
	}
	for _, name := range required {
		if !seen[name] {
			return fmt.Errorf("files is missing required member %q", name)
		}
	}
	if manifest.Limits.WallTimeMS == 0 || manifest.Limits.MaxSteps == 0 || manifest.Limits.MaxResultBytes == 0 || manifest.Limits.MaxProcessOutputBytes == 0 {
		return errors.New("wallTimeMs, maxSteps, maxResultBytes, and maxProcessOutputBytes must be positive")
	}
	if manifest.Limits.WallTimeMS > uint64((1<<63-1)/int64(time.Millisecond)) {
		return errors.New("wallTimeMs exceeds the runtime duration representation")
	}
	maxInt := uint64(^uint(0) >> 1)
	if manifest.Limits.MaxResultBytes > maxInt || manifest.Limits.MaxProcessOutputBytes > maxInt {
		return errors.New("result and process output limits must fit the runtime address space")
	}
	return nil
}

func validateMemberName(name string) error {
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name || name == "." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("package member %q must be one canonical relative slash path", name)
	}
	return nil
}

func validateProgram(name string, source []byte) error {
	file, program, err := starlark.SourceProgramOptions(&syntax.FileOptions{While: true}, name, source, func(identifier string) bool {
		return identifier == "windows" || identifier == "task"
	})
	if err != nil {
		return fmt.Errorf("compile Starlark entrypoint: %w", err)
	}
	if program.NumLoads() != 0 {
		return errors.New("Starlark load statements are forbidden")
	}
	mainCount := 0
	for _, statement := range file.Stmts {
		definition, ok := statement.(*syntax.DefStmt)
		if !ok || definition.Name.Name != "main" {
			continue
		}
		mainCount++
		if len(definition.Params) != 1 {
			return errors.New("Starlark entrypoint main(ctx) must declare exactly one parameter")
		}
		parameter, ok := definition.Params[0].(*syntax.Ident)
		if !ok || parameter.Name != "ctx" {
			return errors.New("Starlark entrypoint main parameter must be named ctx")
		}
	}
	if mainCount != 1 {
		return errors.New("Starlark package must declare exactly one main(ctx) entrypoint")
	}
	return nil
}

func compileSchema(label string, data []byte) (*jsonschema.Schema, error) {
	if err := strictjson.Validate(data); err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denySchemaLoader{})
	url := "https://windowsagent.invalid/windows-automation/" + label
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

type denySchemaLoader struct{}

func (denySchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource is forbidden: %s", url)
}

func decodeStrict(data []byte, target any) error {
	if err := strictjson.Validate(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("JSON contains trailing content")
	}
	return nil
}

func digestMembers(members map[string][]byte) string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	var length [8]byte
	for _, name := range names {
		binary.BigEndian.PutUint64(length[:], uint64(len(name)))
		hash.Write(length[:])
		hash.Write([]byte(name))
		binary.BigEndian.PutUint64(length[:], uint64(len(members[name])))
		hash.Write(length[:])
		hash.Write(members[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneBytes(data []byte) []byte { return append([]byte(nil), data...) }
