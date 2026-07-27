// Package rules resolves foreground executables to trusted Codex guidance.
package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
)

const (
	StatusMatched        = "matched"
	StatusUnmatched      = "unmatched"
	MatchedDescription   = "The executing agent must read rule.agents.url before taking any rule-specific action."
	UnmatchedDescription = "No rule guidance is available for this foreground process."
	agentsFilename       = "AGENTS.md"
	agentsMediaType      = "text/markdown; charset=utf-8"
)

type Document struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
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
		if r.Description != MatchedDescription {
			return errors.New("matched rule description is invalid")
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
		if r.Agents.ContentType != agentsMediaType {
			return errors.New("matched rule AGENTS content type is invalid")
		}
		if len(r.Agents.SHA256) != sha256.Size*2 {
			return errors.New("matched rule AGENTS sha256 must contain 64 hexadecimal characters")
		}
		if _, err := hex.DecodeString(r.Agents.SHA256); err != nil {
			return fmt.Errorf("matched rule AGENTS sha256 is not hexadecimal: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid rule status %q", r.Status)
	}
}

type entry struct {
	id     string
	agents []byte
	sha256 string
}

type Registry struct {
	byExecutable map[string]entry
}

func New(source fs.FS) (*Registry, error) {
	if source == nil {
		return nil, errors.New("rule document filesystem is required")
	}
	directories, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, fmt.Errorf("read rule document root: %w", err)
	}
	registry := &Registry{byExecutable: make(map[string]entry)}
	for _, directory := range directories {
		if !directory.IsDir() {
			return nil, fmt.Errorf("unexpected file in rule document root: %s", directory.Name())
		}
		id := directory.Name()
		if err := validateRuleID(id); err != nil {
			return nil, err
		}
		key := normalizeExecutable(id)
		if _, exists := registry.byExecutable[key]; exists {
			return nil, fmt.Errorf("duplicate rule executable name: %s", id)
		}
		path := id + "/" + agentsFilename
		agents, err := fs.ReadFile(source, path)
		if err != nil {
			return nil, fmt.Errorf("read rule %s AGENTS.md: %w", id, err)
		}
		if len(strings.TrimSpace(string(agents))) == 0 {
			return nil, fmt.Errorf("rule %s AGENTS.md is empty", id)
		}
		sum := sha256.Sum256(agents)
		registry.byExecutable[key] = entry{
			id:     id,
			agents: append([]byte(nil), agents...),
			sha256: hex.EncodeToString(sum[:]),
		}
	}
	if len(registry.byExecutable) == 0 {
		return nil, errors.New("at least one rule document is required")
	}
	return registry, nil
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	return len(r.byExecutable)
}

func (r *Registry) Resolve(executableName string) (Resolution, error) {
	if r == nil {
		return Resolution{}, errors.New("rule registry is required")
	}
	if executableName == "" {
		return Resolution{}, errors.New("foreground executable name is required for rule resolution")
	}
	matched, ok := r.byExecutable[normalizeExecutable(executableName)]
	if !ok {
		return Resolution{
			Status:      StatusUnmatched,
			Description: UnmatchedDescription,
		}, nil
	}
	return Resolution{
		Status:      StatusMatched,
		Description: MatchedDescription,
		ID:          matched.id,
		Agents: &Document{
			URL:         agentsURL(matched.id),
			ContentType: agentsMediaType,
			SHA256:      matched.sha256,
		},
	}, nil
}

func (r *Registry) ReadAGENTS(id string) ([]byte, Resolution, error) {
	if r == nil {
		return nil, Resolution{}, errors.New("rule registry is required")
	}
	matched, ok := r.byExecutable[normalizeExecutable(id)]
	if !ok || matched.id != id {
		return nil, Resolution{}, fs.ErrNotExist
	}
	resolution, err := r.Resolve(id)
	if err != nil {
		return nil, Resolution{}, err
	}
	return append([]byte(nil), matched.agents...), resolution, nil
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

func normalizeExecutable(name string) string {
	return strings.ToLower(name)
}

func agentsURL(id string) string {
	return "/v1/rules/" + url.PathEscape(id) + "/" + agentsFilename
}
