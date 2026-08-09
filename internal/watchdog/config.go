package watchdog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion  = 1
	MaxConfigBytes = 1 << 20
)

var (
	identifierPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	windowsDrivePath   = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	windowsNetworkPath = regexp.MustCompile(`^\\\\[^\\]+\\[^\\]+`)
)

type Config struct {
	SchemaVersion   int      `json:"schemaVersion"`
	CheckIntervalMS uint64   `json:"checkIntervalMs"`
	Targets         []Target `json:"targets"`
}

type Target struct {
	ID                string         `json:"id"`
	DesiredState      string         `json:"desiredState"`
	StartAfterHealthy []string       `json:"startAfterHealthy"`
	FailureThreshold  uint32         `json:"failureThreshold"`
	Probes            []ProbeConfig  `json:"probes"`
	Recovery          RecoveryConfig `json:"recovery"`
}

type ProbeConfig struct {
	Type                      string `json:"type"`
	ExecutablePath            string `json:"executablePath,omitempty"`
	RequireInteractiveSession *bool  `json:"requireInteractiveSession,omitempty"`
	URL                       string `json:"url,omitempty"`
	TimeoutMS                 uint64 `json:"timeoutMs,omitempty"`
	ExpectedStatusCode        int    `json:"expectedStatusCode,omitempty"`
	ExpectedJSONStatus        string `json:"expectedJsonStatus,omitempty"`
}

type RecoveryConfig struct {
	ScheduledTaskName       string `json:"scheduledTaskName"`
	ExpectedTaskDescription string `json:"expectedTaskDescription"`
	MaxAttempts             uint32 `json:"maxAttempts"`
	AttemptWindowMS         uint64 `json:"attemptWindowMs"`
	BackoffMS               uint64 `json:"backoffMs"`
	ActionTimeoutMS         uint64 `json:"actionTimeoutMs"`
	StartupGraceMS          uint64 `json:"startupGraceMs"`
}

func (p *ProbeConfig) UnmarshalJSON(data []byte) error {
	type plainProbe ProbeConfig
	var decoded plainProbe
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]struct{}{"type": {}}
	switch decoded.Type {
	case "process":
		allowed["executablePath"] = struct{}{}
		allowed["requireInteractiveSession"] = struct{}{}
	case "http-json":
		allowed["url"] = struct{}{}
		allowed["timeoutMs"] = struct{}{}
		allowed["expectedStatusCode"] = struct{}{}
		allowed["expectedJsonStatus"] = struct{}{}
	default:
		return fmt.Errorf("unsupported probe type %q", decoded.Type)
	}
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("probe type %q does not allow field %q", decoded.Type, field)
		}
	}
	*p = ProbeConfig(decoded)
	return nil
}

func LoadConfig(name string) (Config, error) {
	info, err := os.Stat(name)
	if err != nil {
		return Config{}, fmt.Errorf("stat watchdog config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("watchdog config must be a regular file")
	}
	if info.Size() == 0 || info.Size() > MaxConfigBytes {
		return Config{}, fmt.Errorf("watchdog config size must be between 1 and %d bytes", MaxConfigBytes)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return Config{}, fmt.Errorf("read watchdog config: %w", err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode watchdog config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Config{}, errors.New("decode watchdog config: multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode watchdog config trailing data: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("watchdog config schemaVersion must be %d", SchemaVersion)
	}
	if c.CheckIntervalMS < 250 || c.CheckIntervalMS > uint64((24*time.Hour)/time.Millisecond) {
		return errors.New("watchdog config checkIntervalMs must be between 250 and 86400000")
	}
	if len(c.Targets) == 0 {
		return errors.New("watchdog config targets must not be empty")
	}
	seen := make(map[string]struct{}, len(c.Targets))
	for index, target := range c.Targets {
		if err := target.validate(); err != nil {
			return fmt.Errorf("watchdog target %d: %w", index, err)
		}
		if _, exists := seen[target.ID]; exists {
			return fmt.Errorf("watchdog target %d: duplicate id %q", index, target.ID)
		}
		seen[target.ID] = struct{}{}
	}
	for index, target := range c.Targets {
		for _, dependency := range target.StartAfterHealthy {
			if _, exists := seen[dependency]; !exists {
				return fmt.Errorf("watchdog target %d: startAfterHealthy references unknown target %q", index, dependency)
			}
		}
	}
	if _, err := c.StartupOrder(); err != nil {
		return err
	}
	return nil
}

func (t Target) validate() error {
	if !identifierPattern.MatchString(t.ID) {
		return errors.New("id must be a lowercase stable identifier")
	}
	if t.DesiredState != "running" {
		return errors.New("desiredState must be explicitly set to running")
	}
	dependencies := make(map[string]struct{}, len(t.StartAfterHealthy))
	for _, dependency := range t.StartAfterHealthy {
		if !identifierPattern.MatchString(dependency) {
			return fmt.Errorf("startAfterHealthy contains invalid target id %q", dependency)
		}
		if dependency == t.ID {
			return errors.New("startAfterHealthy must not reference the target itself")
		}
		if _, exists := dependencies[dependency]; exists {
			return fmt.Errorf("startAfterHealthy contains duplicate target %q", dependency)
		}
		dependencies[dependency] = struct{}{}
	}
	if t.FailureThreshold == 0 {
		return errors.New("failureThreshold must be positive")
	}
	if len(t.Probes) == 0 {
		return errors.New("probes must not be empty")
	}
	for index, probe := range t.Probes {
		if err := probe.validate(); err != nil {
			return fmt.Errorf("probe %d: %w", index, err)
		}
	}
	if err := t.Recovery.validate(); err != nil {
		return fmt.Errorf("recovery: %w", err)
	}
	return nil
}

func (c Config) StartupOrder() ([]Target, error) {
	byID := make(map[string]Target, len(c.Targets))
	for _, target := range c.Targets {
		byID[target.ID] = target
	}
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(c.Targets))
	ordered := make([]Target, 0, len(c.Targets))
	var visit func(string, []string) error
	visit = func(id string, path []string) error {
		switch state[id] {
		case visited:
			return nil
		case visiting:
			return fmt.Errorf("watchdog startup dependency cycle: %s -> %s", strings.Join(path, " -> "), id)
		}
		target, exists := byID[id]
		if !exists {
			return fmt.Errorf("watchdog startup dependency references unknown target %q", id)
		}
		state[id] = visiting
		for _, dependency := range target.StartAfterHealthy {
			if err := visit(dependency, append(path, id)); err != nil {
				return err
			}
		}
		state[id] = visited
		ordered = append(ordered, target)
		return nil
	}
	for _, target := range c.Targets {
		if err := visit(target.ID, nil); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func (p ProbeConfig) validate() error {
	switch p.Type {
	case "process":
		if !isWindowsAbsolutePath(p.ExecutablePath) {
			return errors.New("process executablePath must be an absolute Windows path")
		}
		if p.RequireInteractiveSession == nil {
			return errors.New("process requireInteractiveSession is required")
		}
		if p.URL != "" || p.TimeoutMS != 0 || p.ExpectedStatusCode != 0 || p.ExpectedJSONStatus != "" {
			return errors.New("process probe contains HTTP-only fields")
		}
	case "http-json":
		if p.ExecutablePath != "" || p.RequireInteractiveSession != nil {
			return errors.New("http-json probe contains process-only fields")
		}
		parsed, err := url.Parse(p.URL)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return errors.New("http-json url must be an absolute plain HTTP URL")
		}
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("http-json url must use an explicit loopback IP address")
		}
		if p.TimeoutMS == 0 || p.TimeoutMS > 60_000 {
			return errors.New("http-json timeoutMs must be between 1 and 60000")
		}
		if p.ExpectedStatusCode < 100 || p.ExpectedStatusCode > 599 {
			return errors.New("http-json expectedStatusCode must be between 100 and 599")
		}
		if p.ExpectedJSONStatus == "" || strings.TrimSpace(p.ExpectedJSONStatus) != p.ExpectedJSONStatus {
			return errors.New("http-json expectedJsonStatus must be non-empty without surrounding whitespace")
		}
	default:
		return fmt.Errorf("unsupported probe type %q", p.Type)
	}
	return nil
}

func (r RecoveryConfig) validate() error {
	if r.ScheduledTaskName == "" || strings.TrimSpace(r.ScheduledTaskName) != r.ScheduledTaskName {
		return errors.New("scheduledTaskName must be non-empty without surrounding whitespace")
	}
	if r.ExpectedTaskDescription == "" || strings.TrimSpace(r.ExpectedTaskDescription) != r.ExpectedTaskDescription {
		return errors.New("expectedTaskDescription must be non-empty without surrounding whitespace")
	}
	if r.MaxAttempts == 0 {
		return errors.New("maxAttempts must be positive")
	}
	if r.AttemptWindowMS == 0 || r.BackoffMS == 0 || r.ActionTimeoutMS == 0 || r.StartupGraceMS == 0 {
		return errors.New("attemptWindowMs, backoffMs, actionTimeoutMs, and startupGraceMs must be positive")
	}
	if r.AttemptWindowMS < r.BackoffMS {
		return errors.New("attemptWindowMs must not be shorter than backoffMs")
	}
	if r.ActionTimeoutMS > 300_000 || r.StartupGraceMS > 300_000 {
		return errors.New("actionTimeoutMs and startupGraceMs must not exceed 300000")
	}
	return nil
}

func isWindowsAbsolutePath(value string) bool {
	return windowsDrivePath.MatchString(value) || windowsNetworkPath.MatchString(value)
}
