package ocrworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/qoli/WindowsAgent/internal/rules"
)

type Recognizer interface {
	Recognize(context.Context, string, string, Request) (Result, error)
	DetectTextRegions(context.Context, string, string, Request) (TextRegionsResult, error)
}

type Manager struct {
	mu                 sync.Mutex
	rules              *rules.Store
	runtimeRoot        string
	logger             *slog.Logger
	activeRule         string
	recognitionClients map[string]*Client
	textRegionClients  map[string]*TextRegionsClient
	failed             map[string]error
}

func NewManager(ruleStore *rules.Store, runtimeRoot string, logger *slog.Logger) (*Manager, error) {
	if ruleStore == nil {
		return nil, errors.New("Rule store is required for OCR runtime management")
	}
	if runtimeRoot == "" {
		return nil, errors.New("OCR runtime root is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Manager{
		rules: ruleStore, runtimeRoot: runtimeRoot, logger: logger,
		recognitionClients: map[string]*Client{}, textRegionClients: map[string]*TextRegionsClient{},
		failed: map[string]error{},
	}, nil
}

func (m *Manager) Reconcile(ctx context.Context, executableName string) error {
	if m == nil {
		return errors.New("OCR runtime manager is required")
	}
	resolution, err := m.rules.Resolve(executableName)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if resolution.Status != rules.StatusMatched {
		return m.switchRuleLocked("")
	}
	if m.activeRule != resolution.ID {
		if err := m.switchRuleLocked(resolution.ID); err != nil {
			return err
		}
	}
	profiles, _, err := m.rules.ReadRuntimeProfiles(resolution.ID)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if _, failed := m.failed[profile.ID]; failed {
			continue
		}
		if _, exists := m.recognitionClients[profile.ID]; exists {
			continue
		}
		if _, exists := m.textRegionClients[profile.ID]; exists {
			continue
		}
		if err := m.startLocked(ctx, profile); err != nil {
			m.failed[profile.ID] = err
			m.logger.Error("ocr_worker_start_failed", "rule_id", resolution.ID, "profile_id", profile.ID, "error", err)
		}
	}
	return nil
}

func (m *Manager) Recognize(ctx context.Context, ruleID, profileID string, request Request) (Result, error) {
	if m == nil {
		return Result{}, errors.New("OCR runtime manager is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeRule != ruleID {
		if err := m.switchRuleLocked(ruleID); err != nil {
			return Result{}, err
		}
	}
	if failure := m.failed[profileID]; failure != nil {
		return Result{}, fmt.Errorf("OCR runtime profile %s failed during this Rule activation: %w", profileID, failure)
	}
	client := m.recognitionClients[profileID]
	if client == nil {
		profile, err := m.findProfileLocked(ruleID, profileID)
		if err != nil {
			return Result{}, err
		}
		if profile.Runtime != rules.PpOcrWorkerRuntimeV1 {
			return Result{}, fmt.Errorf("OCR runtime profile %s does not provide recognition-only calls", profileID)
		}
		if err := m.startLocked(ctx, profile); err != nil {
			m.failed[profileID] = err
			return Result{}, err
		}
		client = m.recognitionClients[profileID]
	}
	result, err := client.Recognize(ctx, request)
	if err != nil {
		delete(m.recognitionClients, profileID)
		_ = client.Close()
		m.logger.Warn("ocr_worker_retired_after_call_failure", "rule_id", ruleID, "profile_id", profileID, "error", err)
		return Result{}, err
	}
	return result, nil
}

func (m *Manager) DetectTextRegions(ctx context.Context, ruleID, profileID string, request Request) (TextRegionsResult, error) {
	if m == nil {
		return TextRegionsResult{}, errors.New("OCR runtime manager is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeRule != ruleID {
		if err := m.switchRuleLocked(ruleID); err != nil {
			return TextRegionsResult{}, err
		}
	}
	if failure := m.failed[profileID]; failure != nil {
		return TextRegionsResult{}, fmt.Errorf("OCR runtime profile %s failed during this Rule activation: %w", profileID, failure)
	}
	client := m.textRegionClients[profileID]
	if client == nil {
		profile, err := m.findProfileLocked(ruleID, profileID)
		if err != nil {
			return TextRegionsResult{}, err
		}
		if profile.Runtime != rules.PpOcrTextRegionsWorkerRuntimeV1 {
			return TextRegionsResult{}, fmt.Errorf("OCR runtime profile %s does not provide text region calls", profileID)
		}
		if err := m.startLocked(ctx, profile); err != nil {
			m.failed[profileID] = err
			return TextRegionsResult{}, err
		}
		client = m.textRegionClients[profileID]
	}
	result, err := client.DetectRecognize(ctx, request)
	if err != nil {
		delete(m.textRegionClients, profileID)
		_ = client.Close()
		m.logger.Warn("ocr_text_regions_worker_retired_after_call_failure", "rule_id", ruleID, "profile_id", profileID, "error", err)
		return TextRegionsResult{}, err
	}
	return result, nil
}

func (m *Manager) findProfileLocked(ruleID, profileID string) (rules.RuntimeProfile, error) {
	profiles, _, err := m.rules.ReadRuntimeProfiles(ruleID)
	if err != nil {
		return rules.RuntimeProfile{}, err
	}
	for _, profile := range profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return rules.RuntimeProfile{}, fmt.Errorf("Rule %s does not declare runtime profile %s", ruleID, profileID)
}

func (m *Manager) startLocked(ctx context.Context, profile rules.RuntimeProfile) error {
	if profile.Residency != rules.ResidencyRuleActive {
		return fmt.Errorf("unsupported OCR runtime profile contract: %+v", profile)
	}
	switch profile.Runtime {
	case rules.PpOcrWorkerRuntimeV1:
		client, err := Start(context.Background(), m.runtimeRoot)
		if err != nil {
			return err
		}
		initialized := client.Initialized()
		if initialized.Model.ArtifactID != profile.ArtifactID {
			_ = client.Close()
			return fmt.Errorf("OCR runtime artifact mismatch: Rule=%s worker=%s", profile.ArtifactID, initialized.Model.ArtifactID)
		}
		m.recognitionClients[profile.ID] = client
		m.logger.Info("ocr_worker_started", "rule_id", profile.RuleID, "profile_id", profile.ID,
			"process_id", initialized.ProcessID, "model_load_ms", initialized.ModelLoadMS)
		return nil
	case rules.PpOcrTextRegionsWorkerRuntimeV1:
		client, err := StartTextRegions(context.Background(), m.runtimeRoot)
		if err != nil {
			return err
		}
		initialized := client.Initialized()
		if initialized.Detection.ArtifactID != profile.ArtifactID {
			_ = client.Close()
			return fmt.Errorf("OCR text regions runtime artifact mismatch: Rule=%s worker=%s", profile.ArtifactID, initialized.Detection.ArtifactID)
		}
		m.textRegionClients[profile.ID] = client
		m.logger.Info("ocr_text_regions_worker_started", "rule_id", profile.RuleID, "profile_id", profile.ID,
			"process_id", initialized.ProcessID, "model_load_ms", initialized.ModelLoadMS)
		return nil
	default:
		return fmt.Errorf("unsupported OCR runtime profile contract: %+v", profile)
	}
}

func (m *Manager) switchRuleLocked(ruleID string) error {
	var result error
	for profileID, client := range m.recognitionClients {
		if err := client.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("stop OCR profile %s: %w", profileID, err))
		}
	}
	for profileID, client := range m.textRegionClients {
		if err := client.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("stop OCR text regions profile %s: %w", profileID, err))
		}
	}
	m.recognitionClients = map[string]*Client{}
	m.textRegionClients = map[string]*TextRegionsClient{}
	m.failed = map[string]error{}
	m.activeRule = ruleID
	return result
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.switchRuleLocked("")
}
