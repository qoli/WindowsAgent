// Package rules resolves foreground executables and registered scripts from
// live, externally distributed Rule plugin folders.
package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	StatusMatched        = "matched"
	StatusUnmatched      = "unmatched"
	UnmatchedDescription = "No rule guidance is available for this foreground process."
	RuleFilename         = "rule.json"
	AgentsFilename       = "AGENTS.md"
	AgentsMediaType      = "text/markdown; charset=utf-8"
	ObservationRuntimeV1 = "windows-observation-v1"
	maxRuleJSONBytes     = 64 << 10
	maxAgentsBytes       = 1 << 20
)

type Document struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type Resolution struct {
	Status      string    `json:"status"`
	Description string    `json:"description"`
	ID          string    `json:"id,omitempty"`
	Agents      *Document `json:"agents,omitempty"`
}

func (r Resolution) Validate() error {
	switch r.Status {
	case StatusUnmatched:
		if r.Description != UnmatchedDescription {
			return errors.New("unmatched rule description is invalid")
		}
		if r.ID != "" || r.Agents != nil {
			return errors.New("unmatched rule must not contain an ID or AGENTS document")
		}
		return nil
	case StatusMatched:
		if strings.TrimSpace(r.Description) == "" || strings.TrimSpace(r.Description) != r.Description {
			return errors.New("matched rule description must be non-empty and canonical")
		}
		if err := validateRuleID(r.ID); err != nil {
			return err
		}
		if r.Agents == nil {
			return errors.New("matched rule requires an AGENTS document")
		}
		if r.Agents.URL != agentsURL(r.ID) {
			return errors.New("matched rule AGENTS URL does not match rule ID")
		}
		if r.Agents.ContentType != AgentsMediaType {
			return errors.New("matched rule AGENTS content type is invalid")
		}
		return nil
	default:
		return fmt.Errorf("invalid rule status %q", r.Status)
	}
}

type ScriptDeclaration struct {
	Path    string `json:"path"`
	Runtime string `json:"runtime"`
}

type Descriptor struct {
	SchemaVersion uint32                       `json:"schemaVersion"`
	Description   string                       `json:"description"`
	Scripts       map[string]ScriptDeclaration `json:"scripts"`
}

type Script struct {
	ID      string
	RuleID  string
	Runtime string
	Root    string
}

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("rules directory must be absolute")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve rules directory: %w", err)
	}
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("stat rules directory: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("rules directory must be a directory")
	}
	return &Store{root: canonicalRoot}, nil
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Store) Count() (int, error) {
	entries, err := s.ruleDirectories()
	return len(entries), err
}

func (s *Store) Resolve(executableName string) (Resolution, error) {
	if s == nil {
		return Resolution{}, errors.New("rule store is required")
	}
	if executableName == "" {
		return Resolution{}, errors.New("foreground executable name is required for rule resolution")
	}
	id, found, err := s.findRule(executableName, false)
	if err != nil {
		return Resolution{}, err
	}
	if !found {
		return Resolution{
			Status:      StatusUnmatched,
			Description: UnmatchedDescription,
		}, nil
	}
	descriptor, err := s.readDescriptor(id)
	if err != nil {
		return Resolution{}, err
	}
	if _, err := s.readAgents(id); err != nil {
		return Resolution{}, err
	}
	return matchedResolution(id, descriptor.Description), nil
}

func (s *Store) ReadAGENTS(id string) ([]byte, Resolution, error) {
	if s == nil {
		return nil, Resolution{}, errors.New("rule store is required")
	}
	canonicalID, found, err := s.findRule(id, true)
	if err != nil {
		return nil, Resolution{}, err
	}
	if !found {
		return nil, Resolution{}, fs.ErrNotExist
	}
	descriptor, err := s.readDescriptor(canonicalID)
	if err != nil {
		return nil, Resolution{}, err
	}
	content, err := s.readAgents(canonicalID)
	if err != nil {
		return nil, Resolution{}, err
	}
	return content, matchedResolution(canonicalID, descriptor.Description), nil
}

func (s *Store) ResolveScript(capabilityID string) (Script, error) {
	if s == nil {
		return Script{}, errors.New("rule store is required")
	}
	if strings.TrimSpace(capabilityID) == "" || strings.TrimSpace(capabilityID) != capabilityID {
		return Script{}, errors.New("capability ID is required and must be canonical")
	}
	directories, err := s.ruleDirectories()
	if err != nil {
		return Script{}, err
	}
	var matched *Script
	for _, id := range directories {
		descriptor, err := s.readDescriptor(id)
		if err != nil {
			return Script{}, err
		}
		declaration, ok := descriptor.Scripts[capabilityID]
		if !ok {
			continue
		}
		if matched != nil {
			return Script{}, fmt.Errorf("duplicate script capability ID %q", capabilityID)
		}
		root, err := s.resolveScriptRoot(id, declaration.Path)
		if err != nil {
			return Script{}, fmt.Errorf("resolve rule %s script %s: %w", id, capabilityID, err)
		}
		matched = &Script{
			ID:      capabilityID,
			RuleID:  id,
			Runtime: declaration.Runtime,
			Root:    root,
		}
	}
	if matched == nil {
		return Script{}, fs.ErrNotExist
	}
	return *matched, nil
}

func (s *Store) findRule(executableName string, requireCanonical bool) (string, bool, error) {
	directories, err := s.ruleDirectories()
	if err != nil {
		return "", false, err
	}
	key := normalizeExecutable(executableName)
	for _, id := range directories {
		if normalizeExecutable(id) != key {
			continue
		}
		if requireCanonical && id != executableName {
			return "", false, nil
		}
		return id, true, nil
	}
	return "", false, nil
}

func (s *Store) ruleDirectories() ([]string, error) {
	if s == nil {
		return nil, errors.New("rule store is required")
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read rules directory: %w", err)
	}
	seen := make(map[string]string, len(entries))
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected file in rules directory: %s", entry.Name())
		}
		id := entry.Name()
		if err := validateRuleID(id); err != nil {
			return nil, err
		}
		key := normalizeExecutable(id)
		if previous, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate rule executable name: %s and %s", previous, id)
		}
		seen[key] = id
		result = append(result, id)
	}
	return result, nil
}

func (s *Store) readDescriptor(id string) (Descriptor, error) {
	data, err := readRuleFile(filepath.Join(s.root, id), RuleFilename, maxRuleJSONBytes)
	if err != nil {
		return Descriptor{}, fmt.Errorf("read rule %s %s: %w", id, RuleFilename, err)
	}
	var descriptor Descriptor
	if err := decodeStrictJSON(data, &descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("decode rule %s %s: %w", id, RuleFilename, err)
	}
	if err := validateDescriptor(descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("validate rule %s %s: %w", id, RuleFilename, err)
	}
	return descriptor, nil
}

func (s *Store) readAgents(id string) ([]byte, error) {
	data, err := readRuleFile(filepath.Join(s.root, id), AgentsFilename, maxAgentsBytes)
	if err != nil {
		return nil, fmt.Errorf("read rule %s %s: %w", id, AgentsFilename, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("rule %s %s is empty", id, AgentsFilename)
	}
	return data, nil
}

func (s *Store) resolveScriptRoot(id, name string) (string, error) {
	ruleRoot := filepath.Join(s.root, id)
	fullPath := filepath.Join(ruleRoot, filepath.FromSlash(name))
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(ruleRoot, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("script path resolves outside its Rule plugin")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("script path must resolve to a directory")
	}
	return resolved, nil
}

func validateDescriptor(descriptor Descriptor) error {
	if descriptor.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must equal 1, got %d", descriptor.SchemaVersion)
	}
	if strings.TrimSpace(descriptor.Description) == "" ||
		strings.TrimSpace(descriptor.Description) != descriptor.Description {
		return errors.New("description must be non-empty and canonical")
	}
	if descriptor.Scripts == nil {
		return errors.New("scripts is required")
	}
	for id, declaration := range descriptor.Scripts {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
			return fmt.Errorf("script capability ID %q is not canonical", id)
		}
		if err := validateScriptPath(declaration.Path); err != nil {
			return fmt.Errorf("script %s path: %w", id, err)
		}
		if strings.TrimSpace(declaration.Runtime) == "" ||
			strings.TrimSpace(declaration.Runtime) != declaration.Runtime {
			return fmt.Errorf("script %s runtime is required and must be canonical", id)
		}
	}
	return nil
}

func validateScriptPath(name string) error {
	if name == "" || filepath.IsAbs(name) || path.Clean(name) != name ||
		strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return fmt.Errorf("script path %q is not canonical and relative", name)
	}
	if !strings.HasPrefix(name, "Scripts/") || name == "Scripts/" {
		return fmt.Errorf("script path %q must be below Scripts/", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("script path %q escapes the Rule plugin", name)
	}
	return nil
}

func validateRuleID(id string) error {
	switch {
	case id == "":
		return errors.New("rule ID is required")
	case strings.ContainsAny(id, `/\`):
		return fmt.Errorf("rule ID must be a single executable name: %q", id)
	case !strings.HasSuffix(strings.ToLower(id), ".exe"):
		return fmt.Errorf("rule ID must end in .exe: %q", id)
	default:
		return nil
	}
}

func readRuleFile(root, name string, limit int64) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return nil, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("Rule plugin member resolves outside its folder")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("plugin member must be a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("plugin member exceeds %d bytes", limit)
	}
	return os.ReadFile(resolved)
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

func matchedResolution(id, description string) Resolution {
	return Resolution{
		Status:      StatusMatched,
		Description: description,
		ID:          id,
		Agents: &Document{
			URL:         agentsURL(id),
			ContentType: AgentsMediaType,
		},
	}
}

func normalizeExecutable(name string) string {
	return strings.ToLower(name)
}

func agentsURL(id string) string {
	return "/v1/rules/" + url.PathEscape(id) + "/" + AgentsFilename
}
