// Package releasecatalog defines the public multi-process WindowsAgent release.
package releasecatalog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	SchemaVersion      = 1
	TargetWindowsAMD64 = "windows-amd64"

	ClassBootstrap       = "bootstrap"
	ClassRuntimeRequired = "runtime-required"
	ClassRuntimeOptional = "runtime-optional"
	ClassOperatorTool    = "operator-tool"
	ClassDiagnostic      = "diagnostic"

	SubsystemGUI     = "gui"
	SubsystemConsole = "console"

	maxCatalogBytes = 1 << 20
)

var (
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Spec struct {
	Name      string
	Role      string
	Class     string
	Subsystem string
}

// Specs is the authoritative set of executable assets in one public release.
// The build and release workflows must not maintain a second executable list.
func Specs() []Spec {
	return []Spec{
		{Name: "windows-assist-backend.exe", Role: "assist-backend", Class: ClassBootstrap, Subsystem: SubsystemGUI},
		{Name: "windows-assist-gui.exe", Role: "assist-gui", Class: ClassBootstrap, Subsystem: SubsystemGUI},
		{Name: "windows-capture-agent.exe", Role: "capture-agent", Class: ClassRuntimeRequired, Subsystem: SubsystemGUI},
		{Name: "windows-wgc-worker.exe", Role: "capture-worker", Class: ClassRuntimeRequired, Subsystem: SubsystemConsole},
		{Name: "windows-event-stream.exe", Role: "event-stream", Class: ClassRuntimeRequired, Subsystem: SubsystemGUI},
		{Name: "windows-observation-job.exe", Role: "observation-job", Class: ClassRuntimeRequired, Subsystem: SubsystemConsole},
		{Name: "windows-observation-script-runner.exe", Role: "observation-script-runner", Class: ClassRuntimeRequired, Subsystem: SubsystemConsole},
		{Name: "windows-observer.exe", Role: "observer", Class: ClassRuntimeRequired, Subsystem: SubsystemConsole},
		{Name: "windows-action-osd.exe", Role: "action-osd", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-agent-pi.exe", Role: "delegated-pi", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-agent-pi-component.exe", Role: "delegated-pi-component", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-watchdog.exe", Role: "watchdog", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-visual-log.exe", Role: "visual-log", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-evidence-recorder.exe", Role: "evidence-recorder", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-event-web.exe", Role: "event-web", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-sftp.exe", Role: "sftp", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-tailscale-adapter.exe", Role: "tailscale-adapter", Class: ClassRuntimeOptional, Subsystem: SubsystemGUI},
		{Name: "windows-action-check.exe", Role: "action-check", Class: ClassOperatorTool, Subsystem: SubsystemConsole},
		{Name: "windows-starlark-check.exe", Role: "starlark-check", Class: ClassOperatorTool, Subsystem: SubsystemConsole},
		{Name: "windows-starlark-invoke.exe", Role: "starlark-invoke", Class: ClassOperatorTool, Subsystem: SubsystemConsole},
		{Name: "windows-exec.exe", Role: "structured-exec-client", Class: ClassOperatorTool, Subsystem: SubsystemConsole},
		{Name: "windows-key.exe", Role: "direct-key-client", Class: ClassOperatorTool, Subsystem: SubsystemConsole},
		{Name: "windows-capture-agent-console.exe", Role: "capture-agent-diagnostic", Class: ClassDiagnostic, Subsystem: SubsystemConsole},
	}
}

type Catalog struct {
	SchemaVersion int        `json:"schemaVersion"`
	Version       string     `json:"version"`
	Target        string     `json:"target"`
	Artifacts     []Artifact `json:"artifacts"`
}

type Artifact struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Class     string `json:"class"`
	Subsystem string `json:"peSubsystem"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

func BaseInstallArtifact(artifact Artifact) bool {
	return artifact.Class == ClassBootstrap || artifact.Class == ClassRuntimeRequired || artifact.Role == "tailscale-adapter"
}

// InstallArtifact selects every executable deployed by AssistGUI. Operator
// tools and the console diagnostic remain release assets but are not installed
// into the Windows runtime.
func InstallArtifact(artifact Artifact) bool {
	return artifact.Class == ClassBootstrap || artifact.Class == ClassRuntimeRequired || artifact.Class == ClassRuntimeOptional
}

func Load(r io.Reader) (Catalog, error) {
	limited := io.LimitReader(r, maxCatalogBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Catalog{}, fmt.Errorf("read release catalog: %w", err)
	}
	if len(data) > maxCatalogBytes {
		return Catalog{}, errors.New("release catalog exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode release catalog: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Catalog{}, err
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("release catalog contains multiple JSON values")
		}
		return fmt.Errorf("read release catalog suffix: %w", err)
	}
	return nil
}

func (c Catalog) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion must equal %d", SchemaVersion)
	}
	if !versionPattern.MatchString(c.Version) {
		return fmt.Errorf("version is not canonical: %q", c.Version)
	}
	if c.Target != TargetWindowsAMD64 {
		return fmt.Errorf("target must equal %q", TargetWindowsAMD64)
	}
	expected := specMap()
	if len(c.Artifacts) != len(expected) {
		return fmt.Errorf("release catalog must contain exactly %d artifacts, got %d", len(expected), len(c.Artifacts))
	}
	seen := make(map[string]struct{}, len(c.Artifacts))
	lastName := ""
	for index, artifact := range c.Artifacts {
		if artifact.Name <= lastName {
			return fmt.Errorf("artifacts must be sorted by name at index %d", index)
		}
		lastName = artifact.Name
		if filepath.Base(artifact.Name) != artifact.Name || strings.ContainsAny(artifact.Name, `/\`) || !strings.HasSuffix(artifact.Name, ".exe") {
			return fmt.Errorf("artifact name must be a safe exe basename: %q", artifact.Name)
		}
		if _, duplicate := seen[artifact.Name]; duplicate {
			return fmt.Errorf("duplicate artifact name %q", artifact.Name)
		}
		seen[artifact.Name] = struct{}{}
		spec, ok := expected[artifact.Name]
		if !ok {
			return fmt.Errorf("unexpected release artifact %q", artifact.Name)
		}
		if artifact.Role != spec.Role || artifact.Class != spec.Class || artifact.Subsystem != spec.Subsystem {
			return fmt.Errorf("artifact metadata does not match release specification: %s", artifact.Name)
		}
		if artifact.Bytes <= 0 {
			return fmt.Errorf("artifact bytes must be positive: %s", artifact.Name)
		}
		if !shaPattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("artifact sha256 must be 64 lowercase hexadecimal characters: %s", artifact.Name)
		}
	}
	return nil
}

func Generate(directory, version string) (Catalog, error) {
	if !versionPattern.MatchString(version) {
		return Catalog{}, fmt.Errorf("version is not canonical: %q", version)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return Catalog{}, fmt.Errorf("read release directory: %w", err)
	}
	expected := specMap()
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".exe") {
			continue
		}
		if _, ok := expected[entry.Name()]; !ok {
			return Catalog{}, fmt.Errorf("unexpected executable in release directory: %s", entry.Name())
		}
	}
	artifacts := make([]Artifact, 0, len(expected))
	for _, spec := range Specs() {
		path := filepath.Join(directory, spec.Name)
		info, err := os.Stat(path)
		if err != nil {
			return Catalog{}, fmt.Errorf("stat required release artifact %s: %w", spec.Name, err)
		}
		if !info.Mode().IsRegular() {
			return Catalog{}, fmt.Errorf("release artifact must be a regular file: %s", spec.Name)
		}
		subsystem, err := ReadPESubsystem(path)
		if err != nil {
			return Catalog{}, fmt.Errorf("inspect %s: %w", spec.Name, err)
		}
		if subsystem != spec.Subsystem {
			return Catalog{}, fmt.Errorf("artifact %s subsystem is %s, expected %s", spec.Name, subsystem, spec.Subsystem)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return Catalog{}, fmt.Errorf("hash %s: %w", spec.Name, err)
		}
		artifacts = append(artifacts, Artifact{Name: spec.Name, Role: spec.Role, Class: spec.Class, Subsystem: spec.Subsystem, Bytes: info.Size(), SHA256: digest})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	catalog := Catalog{SchemaVersion: SchemaVersion, Version: version, Target: TargetWindowsAMD64, Artifacts: artifacts}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func WriteJSON(w io.Writer, catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(catalog)
}

func WriteSHA256Sums(w io.Writer, catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	buffer := bufio.NewWriter(w)
	for _, artifact := range catalog.Artifacts {
		if _, err := fmt.Fprintf(buffer, "%s  %s\n", artifact.SHA256, artifact.Name); err != nil {
			return err
		}
	}
	return buffer.Flush()
}

func ValidateSHA256Sums(r io.Reader, catalog Catalog) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	expected := make(map[string]string, len(catalog.Artifacts))
	for _, artifact := range catalog.Artifacts {
		expected[artifact.Name] = artifact.SHA256
	}
	seen := make(map[string]struct{}, len(expected))
	scanner := bufio.NewScanner(io.LimitReader(r, maxCatalogBytes+1))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 67 || line[64:66] != "  " {
			return fmt.Errorf("invalid SHA256SUMS line: %q", line)
		}
		digest, name := line[:64], line[66:]
		if !shaPattern.MatchString(digest) || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("invalid SHA256SUMS entry: %q", line)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate SHA256SUMS entry: %s", name)
		}
		want, ok := expected[name]
		if !ok {
			return fmt.Errorf("unexpected SHA256SUMS artifact: %s", name)
		}
		if digest != want {
			return fmt.Errorf("SHA256SUMS digest disagrees with catalog: %s", name)
		}
		seen[name] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read SHA256SUMS: %w", err)
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("SHA256SUMS contains %d artifacts, expected %d", len(seen), len(expected))
	}
	return nil
}

func VerifyDirectory(directory string, catalog Catalog) error {
	return VerifySelected(directory, catalog, func(Artifact) bool { return true })
}

func VerifySelected(directory string, catalog Catalog, include func(Artifact) bool) error {
	if err := catalog.Validate(); err != nil {
		return err
	}
	if include == nil {
		return errors.New("release artifact selector is required")
	}
	selected := 0
	for _, artifact := range catalog.Artifacts {
		if !include(artifact) {
			continue
		}
		selected++
		path := filepath.Join(directory, artifact.Name)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat release artifact %s: %w", artifact.Name, err)
		}
		if !info.Mode().IsRegular() || info.Size() != artifact.Bytes {
			return fmt.Errorf("release artifact size mismatch: %s", artifact.Name)
		}
		digest, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if digest != artifact.SHA256 {
			return fmt.Errorf("release artifact sha256 mismatch: %s", artifact.Name)
		}
		subsystem, err := ReadPESubsystem(path)
		if err != nil {
			return err
		}
		if subsystem != artifact.Subsystem {
			return fmt.Errorf("release artifact subsystem mismatch: %s", artifact.Name)
		}
	}
	if selected == 0 {
		return errors.New("release artifact selector matched no artifacts")
	}
	return nil
}

func specMap() map[string]Spec {
	result := make(map[string]Spec)
	for _, spec := range Specs() {
		result[spec.Name] = spec
	}
	return result
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
