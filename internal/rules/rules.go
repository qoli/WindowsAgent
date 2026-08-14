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
	"regexp"
	"sort"
	"strings"

	"github.com/qoli/WindowsAgent/internal/strictjson"
)

const (
	StatusMatched                   = "matched"
	StatusUnmatched                 = "unmatched"
	UnmatchedDescription            = "No rule guidance is available for this foreground process."
	RuleFilename                    = "rule.json"
	AgentsFilename                  = "AGENTS.md"
	AgentsMediaType                 = "text/markdown; charset=utf-8"
	ScriptsMediaType                = "application/json; charset=utf-8"
	ActionsMediaType                = "application/json; charset=utf-8"
	RegistrationsMediaType          = "application/json; charset=utf-8"
	RuntimesMediaType               = "application/json; charset=utf-8"
	ObservationRuntimeV1            = "windows-observation-v1"
	PureDecisionRuntimeV1           = "windows-pure-decision-v1"
	PpOcrActionRuntimeV1            = "ppocr-w480-text-v1"
	PpOcrTextRegionsActionRuntimeV1 = "ppocr-text-regions-v1"
	CompositeActionRuntimeV1        = "windows-composite-action-v1"
	StreamingActionRuntimeV1        = "windows-streaming-action-v1"
	WindowsKeyActionRuntimeV1       = "windows-key-action-v1"
	WindowsPointerActionRuntimeV1   = "windows-pointer-action-v1"
	PpOcrWorkerRuntimeV1            = "ppocr-onnx-dml-worker-v1"
	PpOcrTextRegionsWorkerRuntimeV1 = "ppocr-onnx-dml-text-regions-worker-v1"
	ResidencyRuleActive             = "while-rule-active"
	RegistrationMonitor             = "monitor"
	RegistrationReaction            = "reaction"
	ActionExposurePublic            = "public"
	ActionExposureInternal          = "internal"
	CompletionReturn                = "return"
	CompletionStream                = "stream"
	LifecycleLinear                 = "linear"
	LifecycleLoop                   = "loop"
	maxRuleJSONBytes                = 64 << 10
	maxAgentsBytes                  = 1 << 20
)

type Document struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
}

type Resolution struct {
	Status        string    `json:"status"`
	Description   string    `json:"description"`
	ID            string    `json:"id,omitempty"`
	Agents        *Document `json:"agents,omitempty"`
	Scripts       *Document `json:"scripts,omitempty"`
	Actions       *Document `json:"actions,omitempty"`
	Registrations *Document `json:"registrations,omitempty"`
	Runtimes      *Document `json:"runtimes,omitempty"`
}

func (r Resolution) Validate() error {
	switch r.Status {
	case StatusUnmatched:
		if r.Description != UnmatchedDescription {
			return errors.New("unmatched rule description is invalid")
		}
		if r.ID != "" || r.Agents != nil || r.Scripts != nil || r.Actions != nil || r.Registrations != nil || r.Runtimes != nil {
			return errors.New("unmatched rule must not contain an ID or Rule documents")
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
		if r.Scripts == nil {
			return errors.New("matched rule requires a Scripts catalog")
		}
		if r.Scripts.URL != scriptsURL(r.ID) {
			return errors.New("matched rule Scripts URL does not match rule ID")
		}
		if r.Scripts.ContentType != ScriptsMediaType {
			return errors.New("matched rule Scripts content type is invalid")
		}
		if r.Actions == nil {
			return errors.New("matched rule requires an Actions catalog")
		}
		if r.Actions.URL != actionsURL(r.ID) || r.Actions.ContentType != ActionsMediaType {
			return errors.New("matched rule Actions document is invalid")
		}
		if r.Registrations == nil {
			return errors.New("matched rule requires a Registrations catalog")
		}
		if r.Registrations.URL != registrationsURL(r.ID) || r.Registrations.ContentType != RegistrationsMediaType {
			return errors.New("matched rule Registrations document is invalid")
		}
		if r.Runtimes == nil || r.Runtimes.URL != runtimesURL(r.ID) || r.Runtimes.ContentType != RuntimesMediaType {
			return errors.New("matched rule Runtimes document is invalid")
		}
		return nil
	default:
		return fmt.Errorf("invalid rule status %q", r.Status)
	}
}

type ActionDeclaration struct {
	Path           string                      `json:"path"`
	Runtime        string                      `json:"runtime"`
	RuntimeProfile string                      `json:"runtimeProfile,omitempty"`
	Exposure       string                      `json:"exposure,omitempty"`
	Execution      *ActionExecutionDeclaration `json:"execution"`
	RegistrableAs  []string                    `json:"registrableAs"`
}

type EphemeralActionSequenceDeclaration struct {
	AllowedActions []string `json:"allowedActions"`
}

type ActionExecutionDeclaration struct {
	Completion    string `json:"completion"`
	Lifecycle     string `json:"lifecycle,omitempty"`
	Interruptible *bool  `json:"interruptible,omitempty"`
}

type ActionExecution struct {
	Completion    string `json:"completion"`
	Lifecycle     string `json:"lifecycle,omitempty"`
	Interruptible bool   `json:"interruptible"`
}

type RuntimeProfileDeclaration struct {
	Runtime    string `json:"runtime"`
	Residency  string `json:"residency"`
	ArtifactID string `json:"artifactId"`
}

type Descriptor struct {
	SchemaVersion           uint32                               `json:"schemaVersion"`
	Description             string                               `json:"description"`
	RuntimeProfiles         map[string]RuntimeProfileDeclaration `json:"runtimeProfiles"`
	Actions                 map[string]ActionDeclaration         `json:"actions"`
	EphemeralActionSequence *EphemeralActionSequenceDeclaration  `json:"ephemeralActionSequence"`
	Registrations           map[string]RegistrationDeclaration   `json:"registrations"`
}

type Action struct {
	ID               string          `json:"id"`
	RuleID           string          `json:"ruleId"`
	Runtime          string          `json:"runtime"`
	RuntimeProfile   string          `json:"runtimeProfile,omitempty"`
	Execution        ActionExecution `json:"execution"`
	RegistrableAs    []string        `json:"registrableAs"`
	SequenceEligible bool            `json:"sequenceEligible"`
	Exposure         string          `json:"-"`
	Root             string          `json:"-"`
}

type RuntimeProfile struct {
	ID         string `json:"id"`
	RuleID     string `json:"ruleId"`
	Runtime    string `json:"runtime"`
	Residency  string `json:"residency"`
	ArtifactID string `json:"artifactId"`
}

type EventTarget struct {
	Stream    string `json:"stream"`
	EventType string `json:"eventType"`
}

type MonitorTrigger struct {
	IntervalMs uint32      `json:"intervalMs"`
	Emit       EventTarget `json:"emit"`
}

type ReactionTrigger struct {
	Stream    string            `json:"stream"`
	EventType string            `json:"eventType"`
	Match     map[string]string `json:"match"`
}

type RegistrationDeclaration struct {
	Type     string           `json:"type"`
	Action   string           `json:"action"`
	Input    json.RawMessage  `json:"input"`
	Monitor  *MonitorTrigger  `json:"monitor,omitempty"`
	Reaction *ReactionTrigger `json:"reaction,omitempty"`
}

type Registration struct {
	ID       string           `json:"id"`
	RuleID   string           `json:"ruleId"`
	Type     string           `json:"type"`
	ActionID string           `json:"actionId"`
	Input    json.RawMessage  `json:"input"`
	Monitor  *MonitorTrigger  `json:"monitor,omitempty"`
	Reaction *ReactionTrigger `json:"reaction,omitempty"`
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

// RuleIDs returns the canonical executable names owned by this Store.
func (s *Store) RuleIDs() ([]string, error) {
	return s.ruleDirectories()
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
	action, err := s.ResolveAction(capabilityID)
	if err != nil {
		return Script{}, err
	}
	if action.Runtime != ObservationRuntimeV1 {
		return Script{}, fmt.Errorf("action %q runtime %q is not supported by the v1 Script launcher", capabilityID, action.Runtime)
	}
	return Script{ID: action.ID, RuleID: action.RuleID, Runtime: action.Runtime, Root: action.Root}, nil
}

func (s *Store) ResolveAction(actionID string) (Action, error) {
	if s == nil {
		return Action{}, errors.New("rule store is required")
	}
	if err := validateRegistryID(actionID, "action"); err != nil {
		return Action{}, err
	}
	directories, err := s.ruleDirectories()
	if err != nil {
		return Action{}, err
	}
	var matched *Action
	for _, id := range directories {
		descriptor, err := s.readDescriptor(id)
		if err != nil {
			return Action{}, err
		}
		declaration, ok := descriptor.Actions[actionID]
		if !ok {
			continue
		}
		if matched != nil {
			return Action{}, fmt.Errorf("duplicate action ID %q", actionID)
		}
		root, err := s.resolveActionRoot(id, declaration.Path)
		if err != nil {
			return Action{}, fmt.Errorf("resolve rule %s action %s: %w", id, actionID, err)
		}
		matched = &Action{
			ID:               actionID,
			RuleID:           id,
			Runtime:          declaration.Runtime,
			RuntimeProfile:   declaration.RuntimeProfile,
			Execution:        resolvedActionExecution(declaration.Execution),
			RegistrableAs:    append([]string(nil), declaration.RegistrableAs...),
			SequenceEligible: slicesContains(descriptor.EphemeralActionSequence.AllowedActions, actionID),
			Exposure:         resolvedActionExposure(declaration.Exposure),
			Root:             root,
		}
	}
	if matched == nil {
		return Action{}, fs.ErrNotExist
	}
	return *matched, nil
}

// ResolvePublicAction resolves only Actions that form the remote Rule API.
// Same-Rule composite and streaming child calls use ResolveAction so that a
// Rule can keep implementation Actions out of catalogs and direct invocation.
func (s *Store) ResolvePublicAction(actionID string) (Action, error) {
	action, err := s.ResolveAction(actionID)
	if err != nil {
		return Action{}, err
	}
	if action.Exposure != ActionExposurePublic {
		return Action{}, fs.ErrNotExist
	}
	return action, nil
}

func (s *Store) ReadScripts(id string) ([]Script, Resolution, error) {
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
	if _, err := s.readAgents(canonicalID); err != nil {
		return nil, Resolution{}, err
	}
	capabilityIDs := make([]string, 0, len(descriptor.Actions))
	for capabilityID, declaration := range descriptor.Actions {
		if declaration.Runtime != ObservationRuntimeV1 || resolvedActionExposure(declaration.Exposure) != ActionExposurePublic {
			continue
		}
		capabilityIDs = append(capabilityIDs, capabilityID)
	}
	sort.Strings(capabilityIDs)
	scripts := make([]Script, 0, len(capabilityIDs))
	for _, capabilityID := range capabilityIDs {
		script, err := s.ResolveScript(capabilityID)
		if err != nil {
			return nil, Resolution{}, fmt.Errorf("resolve script %s: %w", capabilityID, err)
		}
		if script.RuleID != canonicalID {
			return nil, Resolution{}, fmt.Errorf(
				"script %s resolved to Rule %s, expected %s",
				capabilityID,
				script.RuleID,
				canonicalID,
			)
		}
		scripts = append(scripts, script)
	}
	return scripts, matchedResolution(canonicalID, descriptor.Description), nil
}

func (s *Store) ReadActions(id string) ([]Action, Resolution, error) {
	return s.readActions(id, true)
}

// ReadAllActions is for repository/runtime validation that must include
// Rule-internal implementation Actions. It is not an HTTP catalog surface.
func (s *Store) ReadAllActions(id string) ([]Action, Resolution, error) {
	return s.readActions(id, false)
}

func (s *Store) readActions(id string, publicOnly bool) ([]Action, Resolution, error) {
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
	if _, err := s.readAgents(canonicalID); err != nil {
		return nil, Resolution{}, err
	}
	actionIDs := make([]string, 0, len(descriptor.Actions))
	for actionID, declaration := range descriptor.Actions {
		if publicOnly && resolvedActionExposure(declaration.Exposure) != ActionExposurePublic {
			continue
		}
		actionIDs = append(actionIDs, actionID)
	}
	sort.Strings(actionIDs)
	actions := make([]Action, 0, len(actionIDs))
	for _, actionID := range actionIDs {
		action, err := s.ResolveAction(actionID)
		if err != nil {
			return nil, Resolution{}, fmt.Errorf("resolve action %s: %w", actionID, err)
		}
		if action.RuleID != canonicalID {
			return nil, Resolution{}, fmt.Errorf("action %s resolved to Rule %s, expected %s", actionID, action.RuleID, canonicalID)
		}
		actions = append(actions, action)
	}
	return actions, matchedResolution(canonicalID, descriptor.Description), nil
}

func (s *Store) ReadRegistrations(id string) ([]Registration, Resolution, error) {
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
	if _, err := s.readAgents(canonicalID); err != nil {
		return nil, Resolution{}, err
	}
	ids := make([]string, 0, len(descriptor.Registrations))
	for id := range descriptor.Registrations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	registrations := make([]Registration, 0, len(ids))
	for _, id := range ids {
		declaration := descriptor.Registrations[id]
		registrations = append(registrations, Registration{
			ID: id, RuleID: canonicalID, Type: declaration.Type, ActionID: declaration.Action,
			Input: append(json.RawMessage(nil), declaration.Input...), Monitor: declaration.Monitor, Reaction: declaration.Reaction,
		})
	}
	return registrations, matchedResolution(canonicalID, descriptor.Description), nil
}

func (s *Store) ReadRuntimeProfiles(id string) ([]RuntimeProfile, Resolution, error) {
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
	ids := make([]string, 0, len(descriptor.RuntimeProfiles))
	for profileID := range descriptor.RuntimeProfiles {
		ids = append(ids, profileID)
	}
	sort.Strings(ids)
	profiles := make([]RuntimeProfile, 0, len(ids))
	for _, profileID := range ids {
		declaration := descriptor.RuntimeProfiles[profileID]
		profiles = append(profiles, RuntimeProfile{
			ID: profileID, RuleID: canonicalID, Runtime: declaration.Runtime,
			Residency: declaration.Residency, ArtifactID: declaration.ArtifactID,
		})
	}
	return profiles, matchedResolution(canonicalID, descriptor.Description), nil
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
			if entry.Name() == AgentsFilename {
				continue
			}
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

func (s *Store) resolveActionRoot(id, name string) (string, error) {
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
		return "", errors.New("action path resolves outside its Rule plugin")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("action path must resolve to a directory")
	}
	return resolved, nil
}

func validateDescriptor(descriptor Descriptor) error {
	if descriptor.SchemaVersion != 6 {
		return fmt.Errorf("schemaVersion must equal 6, got %d", descriptor.SchemaVersion)
	}
	if strings.TrimSpace(descriptor.Description) == "" ||
		strings.TrimSpace(descriptor.Description) != descriptor.Description {
		return errors.New("description must be non-empty and canonical")
	}
	if descriptor.Actions == nil {
		return errors.New("actions is required")
	}
	if descriptor.Registrations == nil {
		return errors.New("registrations is required")
	}
	if descriptor.EphemeralActionSequence == nil || descriptor.EphemeralActionSequence.AllowedActions == nil {
		return errors.New("ephemeralActionSequence.allowedActions is required")
	}
	if descriptor.RuntimeProfiles == nil {
		return errors.New("runtimeProfiles is required")
	}
	for id, profile := range descriptor.RuntimeProfiles {
		if err := validateRegistryID(id, "runtime profile"); err != nil {
			return err
		}
		if profile.Runtime != PpOcrWorkerRuntimeV1 && profile.Runtime != PpOcrTextRegionsWorkerRuntimeV1 {
			return fmt.Errorf("runtime profile %s runtime must equal %s or %s", id, PpOcrWorkerRuntimeV1, PpOcrTextRegionsWorkerRuntimeV1)
		}
		if profile.Residency != ResidencyRuleActive {
			return fmt.Errorf("runtime profile %s residency must equal %s", id, ResidencyRuleActive)
		}
		if err := validateRegistryID(profile.ArtifactID, "runtime artifact"); err != nil {
			return fmt.Errorf("runtime profile %s: %w", id, err)
		}
	}
	for id, declaration := range descriptor.Actions {
		if err := validateRegistryID(id, "action"); err != nil {
			return err
		}
		if err := validateActionPath(declaration.Path); err != nil {
			return fmt.Errorf("action %s path: %w", id, err)
		}
		if strings.TrimSpace(declaration.Runtime) == "" ||
			strings.TrimSpace(declaration.Runtime) != declaration.Runtime {
			return fmt.Errorf("action %s runtime is required and must be canonical", id)
		}
		if declaration.RegistrableAs == nil {
			return fmt.Errorf("action %s registrableAs is required", id)
		}
		if declaration.Exposure != "" && declaration.Exposure != ActionExposurePublic && declaration.Exposure != ActionExposureInternal {
			return fmt.Errorf("action %s exposure must equal %q or %q", id, ActionExposurePublic, ActionExposureInternal)
		}
		if resolvedActionExposure(declaration.Exposure) == ActionExposureInternal && len(declaration.RegistrableAs) != 0 {
			return fmt.Errorf("internal action %s cannot declare registration eligibility", id)
		}
		if declaration.Runtime == PureDecisionRuntimeV1 && resolvedActionExposure(declaration.Exposure) != ActionExposureInternal {
			return fmt.Errorf("pure decision action %s must be internal", id)
		}
		if err := validateActionExecution(declaration.Execution); err != nil {
			return fmt.Errorf("action %s execution: %w", id, err)
		}
		if declaration.Runtime == PpOcrActionRuntimeV1 || declaration.Runtime == PpOcrTextRegionsActionRuntimeV1 {
			profile, exists := descriptor.RuntimeProfiles[declaration.RuntimeProfile]
			if declaration.RuntimeProfile == "" || !exists {
				return fmt.Errorf("action %s requires a declared runtimeProfile", id)
			}
			expectedRuntime := PpOcrWorkerRuntimeV1
			if declaration.Runtime == PpOcrTextRegionsActionRuntimeV1 {
				expectedRuntime = PpOcrTextRegionsWorkerRuntimeV1
			}
			if profile.Runtime != expectedRuntime {
				return fmt.Errorf("action %s runtimeProfile must use %s", id, expectedRuntime)
			}
		} else if declaration.RuntimeProfile != "" {
			return fmt.Errorf("action %s runtimeProfile is only supported by OCR Action runtimes", id)
		}
		seen := map[string]struct{}{}
		for _, registrationType := range declaration.RegistrableAs {
			if err := validateRegistrationType(registrationType); err != nil {
				return fmt.Errorf("action %s registrableAs: %w", id, err)
			}
			if _, duplicate := seen[registrationType]; duplicate {
				return fmt.Errorf("action %s registrableAs contains duplicate %q", id, registrationType)
			}
			seen[registrationType] = struct{}{}
		}
	}
	sequenceSeen := make(map[string]struct{}, len(descriptor.EphemeralActionSequence.AllowedActions))
	for _, actionID := range descriptor.EphemeralActionSequence.AllowedActions {
		if err := validateRegistryID(actionID, "ephemeral Action Sequence action"); err != nil {
			return err
		}
		declaration, exists := descriptor.Actions[actionID]
		if !exists {
			return fmt.Errorf("ephemeralActionSequence references unknown action %q", actionID)
		}
		if resolvedActionExposure(declaration.Exposure) != ActionExposurePublic {
			return fmt.Errorf("ephemeralActionSequence action %q must be public", actionID)
		}
		if _, duplicate := sequenceSeen[actionID]; duplicate {
			return fmt.Errorf("ephemeralActionSequence contains duplicate action %q", actionID)
		}
		sequenceSeen[actionID] = struct{}{}
		if !coreSequenceRuntime(declaration.Runtime) {
			return fmt.Errorf("ephemeralActionSequence action %q uses unsupported runtime %q", actionID, declaration.Runtime)
		}
		if declaration.Execution.Completion == CompletionStream &&
			(declaration.Execution.Lifecycle != LifecycleLinear || declaration.Execution.Interruptible == nil || !*declaration.Execution.Interruptible) {
			return fmt.Errorf("ephemeralActionSequence streaming action %q must be linear and interruptible", actionID)
		}
	}
	for id, registration := range descriptor.Registrations {
		if err := validateRegistryID(id, "registration"); err != nil {
			return err
		}
		action, exists := descriptor.Actions[registration.Action]
		if !exists {
			return fmt.Errorf("registration %s references unknown action %q", id, registration.Action)
		}
		if resolvedActionExposure(action.Exposure) != ActionExposurePublic {
			return fmt.Errorf("registration %s cannot reference internal action %q", id, registration.Action)
		}
		if err := validateRegistrationType(registration.Type); err != nil {
			return fmt.Errorf("registration %s: %w", id, err)
		}
		if !slicesContains(action.RegistrableAs, registration.Type) {
			return fmt.Errorf("registration %s type %q is not declared by action %q", id, registration.Type, registration.Action)
		}
		if err := validateRegistration(id, registration); err != nil {
			return err
		}
	}
	return nil
}

func resolvedActionExposure(exposure string) string {
	if exposure == "" {
		return ActionExposurePublic
	}
	return exposure
}

func coreSequenceRuntime(runtime string) bool {
	switch runtime {
	case ObservationRuntimeV1, PpOcrActionRuntimeV1, PpOcrTextRegionsActionRuntimeV1,
		CompositeActionRuntimeV1, StreamingActionRuntimeV1, WindowsKeyActionRuntimeV1, WindowsPointerActionRuntimeV1:
		return true
	default:
		return false
	}
}

func validateActionExecution(execution *ActionExecutionDeclaration) error {
	if execution == nil {
		return errors.New("object is required")
	}
	switch execution.Completion {
	case CompletionReturn:
		if execution.Lifecycle != "" || execution.Interruptible != nil {
			return errors.New("return completion forbids lifecycle and interruptible")
		}
	case CompletionStream:
		if execution.Lifecycle != LifecycleLinear && execution.Lifecycle != LifecycleLoop {
			return fmt.Errorf("stream completion lifecycle must equal %q or %q", LifecycleLinear, LifecycleLoop)
		}
		if execution.Interruptible == nil {
			return errors.New("stream completion requires explicit interruptible")
		}
	default:
		return fmt.Errorf("completion must equal %q or %q", CompletionReturn, CompletionStream)
	}
	return nil
}

func resolvedActionExecution(declaration *ActionExecutionDeclaration) ActionExecution {
	execution := ActionExecution{Completion: declaration.Completion, Lifecycle: declaration.Lifecycle}
	if declaration.Interruptible != nil {
		execution.Interruptible = *declaration.Interruptible
	}
	return execution
}

func validateRegistrationType(value string) error {
	switch value {
	case RegistrationMonitor, RegistrationReaction:
		return nil
	default:
		return fmt.Errorf("unsupported registration type %q", value)
	}
}

func validateActionPath(name string) error {
	if name == "" || filepath.IsAbs(name) || path.Clean(name) != name ||
		strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return fmt.Errorf("action path %q is not canonical and relative", name)
	}
	prefix := "Actions/"
	if !strings.HasPrefix(name, prefix) || name == prefix {
		return fmt.Errorf("action path %q must be below %s", name, prefix)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return fmt.Errorf("action path %q escapes the Rule plugin", name)
	}
	return nil
}

func validateRegistryID(id, kind string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id || strings.ContainsAny(id, `\`) {
		return fmt.Errorf("%s ID %q is not canonical", kind, id)
	}
	return nil
}

func validateRegistration(id string, registration RegistrationDeclaration) error {
	if len(registration.Input) == 0 {
		return fmt.Errorf("registration %s input object is required", id)
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(registration.Input, &input); err != nil || input == nil {
		return fmt.Errorf("registration %s input must be an object", id)
	}
	switch registration.Type {
	case RegistrationMonitor:
		if registration.Monitor == nil || registration.Reaction != nil {
			return fmt.Errorf("monitor registration %s requires monitor and forbids reaction", id)
		}
		if registration.Monitor.IntervalMs == 0 {
			return fmt.Errorf("monitor registration %s intervalMs must be positive", id)
		}
		if err := validateEventTarget(registration.Monitor.Emit); err != nil {
			return fmt.Errorf("monitor registration %s emit: %w", id, err)
		}
	case RegistrationReaction:
		if registration.Reaction == nil || registration.Monitor != nil {
			return fmt.Errorf("reaction registration %s requires reaction and forbids monitor", id)
		}
		if err := validateCanonicalString(registration.Reaction.Stream, "stream"); err != nil {
			return fmt.Errorf("reaction registration %s: %w", id, err)
		}
		if err := validateCanonicalString(registration.Reaction.EventType, "eventType"); err != nil {
			return fmt.Errorf("reaction registration %s: %w", id, err)
		}
		if registration.Reaction.Match == nil {
			return fmt.Errorf("reaction registration %s match object is required", id)
		}
		for field, expression := range registration.Reaction.Match {
			if err := validateCanonicalString(field, "match field"); err != nil {
				return fmt.Errorf("reaction registration %s: %w", id, err)
			}
			if _, err := regexp.Compile(expression); err != nil {
				return fmt.Errorf("reaction registration %s match %s regex is invalid: %w", id, field, err)
			}
		}
	}
	return nil
}

func validateEventTarget(target EventTarget) error {
	if err := validateCanonicalString(target.Stream, "stream"); err != nil {
		return err
	}
	return validateCanonicalString(target.EventType, "eventType")
}

func validateCanonicalString(value, name string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and canonical", name)
	}
	return nil
}

func slicesContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
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
		Scripts: &Document{
			URL:         scriptsURL(id),
			ContentType: ScriptsMediaType,
		},
		Actions: &Document{
			URL:         actionsURL(id),
			ContentType: ActionsMediaType,
		},
		Registrations: &Document{
			URL:         registrationsURL(id),
			ContentType: RegistrationsMediaType,
		},
		Runtimes: &Document{
			URL:         runtimesURL(id),
			ContentType: RuntimesMediaType,
		},
	}
}

func normalizeExecutable(name string) string {
	return strings.ToLower(name)
}

func agentsURL(id string) string {
	return "/v1/rules/" + url.PathEscape(id) + "/" + AgentsFilename
}

func scriptsURL(id string) string {
	return "/v1/rules/" + url.PathEscape(id) + "/scripts"
}

func actionsURL(id string) string {
	return "/v3/rules/" + url.PathEscape(id) + "/actions"
}

func registrationsURL(id string) string {
	return "/v3/rules/" + url.PathEscape(id) + "/registrations"
}

func runtimesURL(id string) string {
	return "/v4/rules/" + url.PathEscape(id) + "/runtimes"
}
