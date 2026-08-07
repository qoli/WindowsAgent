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
}

type Manager struct {
	mu          sync.Mutex
	rules       *rules.Store
	runtimeRoot string
	logger      *slog.Logger
	activeRule  string
	clients     map[string]*Client
	failed      map[string]error
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
		clients: map[string]*Client{}, failed: map[string]error{},
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
		if _, exists := m.clients[profile.ID]; exists {
			continue
		}
		if _, err := m.startLocked(ctx, profile); err != nil {
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
	client := m.clients[profileID]
	if client == nil {
		profiles, _, err := m.rules.ReadRuntimeProfiles(ruleID)
		if err != nil {
			return Result{}, err
		}
		var profile *rules.RuntimeProfile
		for index := range profiles {
			if profiles[index].ID == profileID {
				profile = &profiles[index]
				break
			}
		}
		if profile == nil {
			return Result{}, fmt.Errorf("Rule %s does not declare runtime profile %s", ruleID, profileID)
		}
		client, err = m.startLocked(ctx, *profile)
		if err != nil {
			m.failed[profileID] = err
			return Result{}, err
		}
	}
	result, err := client.Recognize(ctx, request)
	if err != nil {
		m.failed[profileID] = err
		delete(m.clients, profileID)
		_ = client.Close()
		return Result{}, err
	}
	return result, nil
}

func (m *Manager) startLocked(ctx context.Context, profile rules.RuntimeProfile) (*Client, error) {
	if profile.Runtime != rules.PpOcrWorkerRuntimeV1 || profile.Residency != rules.ResidencyRuleActive {
		return nil, fmt.Errorf("unsupported OCR runtime profile contract: %+v", profile)
	}
	client, err := Start(context.Background(), m.runtimeRoot)
	if err != nil {
		return nil, err
	}
	initialized := client.Initialized()
	if initialized.Model.ArtifactID != profile.ArtifactID {
		_ = client.Close()
		return nil, fmt.Errorf(
			"OCR runtime artifact mismatch: Rule=%s worker=%s",
			profile.ArtifactID, initialized.Model.ArtifactID,
		)
	}
	m.clients[profile.ID] = client
	m.logger.Info("ocr_worker_started", "rule_id", profile.RuleID, "profile_id", profile.ID,
		"process_id", initialized.ProcessID, "model_load_ms", initialized.ModelLoadMS)
	return client, nil
}

func (m *Manager) switchRuleLocked(ruleID string) error {
	var result error
	for profileID, client := range m.clients {
		if err := client.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("stop OCR profile %s: %w", profileID, err))
		}
	}
	m.clients = map[string]*Client{}
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
