package inputaction

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

const activePresetFilename = "StartPreset.4.start"

const recentReleasedLeaseLimit = 64

type Controller struct {
	frontierBindingsRoot string
	driver               windowsinput.Driver
	foreground           func() (foreground.Info, error)
	mu                   sync.Mutex
	activeLease          *keyLease
	recentReleasedLeases map[string]*keyLease
	releasedLeaseOrder   []string
}

const DirectSchemaVersion = 1

// ExpectedForeground pins one direct key press to the foreground process that
// the caller deliberately observed before requesting input.
type ExpectedForeground struct {
	ProcessID      uint32 `json:"processId"`
	ExecutableName string `json:"executableName"`
	ExecutablePath string `json:"executablePath"`
}

// DirectPressRequest exposes the game-neutral key runtime without requiring a
// Rule package. It deliberately retains the existing finite press bounds.
type DirectPressRequest struct {
	SchemaVersion      uint32             `json:"schemaVersion"`
	ExpectedForeground ExpectedForeground `json:"expectedForeground"`
	Key                string             `json:"key"`
	HoldMS             uint32             `json:"holdMs"`
}

type DirectPressResult struct {
	SchemaVersion uint32             `json:"schemaVersion"`
	Operation     string             `json:"operation"`
	Foreground    ExpectedForeground `json:"foreground"`
	Key           string             `json:"key"`
	Backend       string             `json:"backend"`
	ScanCode      uint16             `json:"scanCode"`
	Extended      bool               `json:"extended"`
	HoldMS        int64              `json:"holdMs"`
}

// Error preserves a stable machine-readable failure boundary for direct key
// input while retaining the underlying diagnostic cause.
type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	if e == nil || e.Cause == nil {
		return "direct key input failed"
	}
	return e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (request DirectPressRequest) Validate() error {
	if request.SchemaVersion != DirectSchemaVersion {
		return errors.New("direct key press schemaVersion must equal 1")
	}
	if request.ExpectedForeground.ProcessID == 0 || request.ExpectedForeground.ExecutableName == "" || request.ExpectedForeground.ExecutablePath == "" ||
		strings.TrimSpace(request.ExpectedForeground.ExecutableName) != request.ExpectedForeground.ExecutableName ||
		strings.ContainsAny(request.ExpectedForeground.ExecutableName, `\\/`) ||
		strings.TrimSpace(request.ExpectedForeground.ExecutablePath) != request.ExpectedForeground.ExecutablePath ||
		!isAbsoluteWindowsPath(request.ExpectedForeground.ExecutablePath) ||
		!strings.EqualFold(windowsPathBase(request.ExpectedForeground.ExecutablePath), request.ExpectedForeground.ExecutableName) {
		return errors.New("expectedForeground requires a positive processId and matching canonical executableName and absolute executablePath")
	}
	if _, err := windowsinput.VirtualKey(request.Key); err != nil {
		return fmt.Errorf("validate direct key: %w", err)
	}
	if request.HoldMS < 1 || request.HoldMS > 1000 {
		return errors.New("direct key press holdMs must be between 1 and 1000")
	}
	return nil
}

// PressDirect executes one literal canonical key press against an exact
// caller-observed foreground process. Success proves injection, not an
// application or user-goal postcondition.
func (c *Controller) PressDirect(ctx context.Context, request DirectPressRequest) (DirectPressResult, error) {
	if c == nil || ctx == nil {
		return DirectPressResult{}, errors.New("input Action controller and context are required")
	}
	if err := request.Validate(); err != nil {
		return DirectPressResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	evidence, observed, err := c.pressLocked(ctx, resolvedBinding{key: request.Key}, request.HoldMS, request.ExpectedForeground, nil)
	if err != nil {
		return DirectPressResult{}, err
	}
	return DirectPressResult{
		SchemaVersion: DirectSchemaVersion, Operation: "press", Foreground: foregroundIdentity(observed),
		Key: evidence.Key, Backend: evidence.Backend, ScanCode: evidence.ScanCode,
		Extended: evidence.Extended, HoldMS: evidence.HoldMS,
	}, nil
}

type keyLease struct {
	id            string
	ruleID        string
	selection     string
	resolved      []resolvedBinding
	evidence      []windowsinput.Evidence
	leaseMS       uint32
	timer         *time.Timer
	state         string
	releaseReason string
	releaseErr    error
	generation    uint64
}

func NewController(frontierBindingsRoot string, driver windowsinput.Driver, foregroundSnapshot func() (foreground.Info, error)) (*Controller, error) {
	if frontierBindingsRoot != "" && !filepath.IsAbs(frontierBindingsRoot) {
		return nil, errors.New("configured Frontier bindings root must be absolute")
	}
	if driver == nil || foregroundSnapshot == nil {
		return nil, errors.New("Windows input driver and foreground resolver are required")
	}
	return &Controller{
		frontierBindingsRoot: frontierBindingsRoot,
		driver:               driver,
		foreground:           foregroundSnapshot,
		recentReleasedLeases: make(map[string]*keyLease),
	}, nil
}

func (c *Controller) Run(ctx context.Context, pkg *Package, inputs map[string]any, ruleID string) (json.RawMessage, error) {
	if c == nil || ctx == nil || pkg == nil {
		return nil, errors.New("input Action controller, context, and package are required")
	}
	if inputs == nil {
		return nil, errors.New("input Action inputs object is required")
	}
	if err := pkg.ValidateInput(inputs); err != nil {
		return nil, fmt.Errorf("validate input Action inputs: %w", err)
	}
	selection, err := selectBinding(pkg.Manifest.Selector, inputs)
	if err != nil {
		return nil, err
	}
	binding, ok := pkg.Manifest.Bindings[selection]
	if !ok {
		return nil, fmt.Errorf("input Action selector resolved undeclared binding %q", selection)
	}
	if pkg.Manifest.Gesture.Type == "lease" {
		return c.runLease(ctx, pkg, inputs, ruleID, selection, binding)
	}
	holdMS, err := resolveHoldMS(pkg.Manifest.Gesture, inputs)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	before, err := c.foreground()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground before input Action: %w", err)
	}
	if !strings.EqualFold(before.ExecutableName, ruleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", before.ExecutableName, ruleID)
	}
	resolved, err := c.resolveBinding(pkg.Manifest.BindingSource, binding)
	if err != nil {
		return nil, err
	}
	revalidated, err := c.resolveBinding(pkg.Manifest.BindingSource, binding)
	if err != nil {
		return nil, fmt.Errorf("revalidate input Action binding before injection: %w", err)
	}
	if revalidated != resolved {
		return nil, errors.New("input Action binding changed before injection")
	}
	evidence, _, err := c.pressLocked(ctx, resolved, holdMS, ExpectedForeground{ExecutableName: ruleID}, &before)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"schemaVersion": 1, "selection": selection,
		"bindingSource": pkg.Manifest.BindingSource.Type,
		"key":           resolved.key, "backend": evidence.Backend,
		"scanCode": int64(evidence.ScanCode), "extended": evidence.Extended, "holdMs": evidence.HoldMS,
	}
	if resolved.control != "" {
		result["control"] = resolved.control
	}
	if resolved.preset != "" {
		result["activePreset"] = resolved.preset
		result["bindingFile"] = resolved.filename
	}
	if err := pkg.ValidateOutput(result); err != nil {
		return nil, fmt.Errorf("validate input Action output: %w", err)
	}
	return json.Marshal(result)
}

func (c *Controller) pressLocked(ctx context.Context, resolved resolvedBinding, holdMS uint32, expected ExpectedForeground, observedBefore *foreground.Info) (windowsinput.Evidence, foreground.Info, error) {
	var before foreground.Info
	if observedBefore == nil {
		observed, err := c.foreground()
		if err != nil {
			return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_FOREGROUND_UNAVAILABLE", "resolving-foreground", fmt.Errorf("resolve foreground before input Action: %w", err))
		}
		before = observed
	} else {
		before = *observedBefore
	}
	if !matchesExpectedForeground(before, expected) {
		return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_FOREGROUND_MISMATCH", "validating-foreground", fmt.Errorf(
			"foreground process is %s pid %d, expected %s pid %d",
			before.ExecutableName, before.ProcessID, expected.ExecutableName, expected.ProcessID,
		))
	}
	current, err := c.foreground()
	if err != nil {
		return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_FOREGROUND_UNAVAILABLE", "revalidating-foreground", fmt.Errorf("revalidate foreground before input injection: %w", err))
	}
	if !sameForeground(before, current) || !matchesExpectedForeground(current, expected) {
		return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_FOREGROUND_CHANGED", "revalidating-foreground", errors.New("foreground process changed before input injection"))
	}
	if c.activeLease != nil {
		for _, held := range c.activeLease.resolved {
			if held.key == resolved.key {
				return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_KEY_CONFLICT", "preflighting-input", fmt.Errorf("press Action key %s conflicts with active key hold lease %q", resolved.key, c.activeLease.id))
			}
		}
	}
	evidence, err := c.driver.Press(ctx, windowsinput.PressRequest{Key: resolved.key, Hold: time.Duration(holdMS) * time.Millisecond})
	if err != nil {
		var releaseError *windowsinput.ReleaseError
		if errors.As(err, &releaseError) {
			return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_RELEASE_FAILED", "releasing-input", fmt.Errorf("press input Action key %s: %w", resolved.key, err))
		}
		return windowsinput.Evidence{}, foreground.Info{}, directError("INPUT_INJECTION_FAILED", "injecting-input", fmt.Errorf("press input Action key %s: %w", resolved.key, err))
	}
	return evidence, current, nil
}

func directError(code, stage string, cause error) error {
	return &Error{Code: code, Stage: stage, Cause: cause}
}

func matchesExpectedForeground(observed foreground.Info, expected ExpectedForeground) bool {
	return strings.EqualFold(observed.ExecutableName, expected.ExecutableName) &&
		(expected.ProcessID == 0 || observed.ProcessID == expected.ProcessID) &&
		(expected.ExecutablePath == "" || strings.EqualFold(filepath.Clean(observed.ExecutablePath), filepath.Clean(expected.ExecutablePath)))
}

func foregroundIdentity(info foreground.Info) ExpectedForeground {
	return ExpectedForeground{ProcessID: info.ProcessID, ExecutableName: info.ExecutableName, ExecutablePath: info.ExecutablePath}
}

func isAbsoluteWindowsPath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func windowsPathBase(value string) string {
	if index := strings.LastIndexAny(value, `\\/`); index >= 0 {
		return value[index+1:]
	}
	return value
}

func (c *Controller) runLease(ctx context.Context, pkg *Package, inputs map[string]any, ruleID, selection string, binding Binding) (json.RawMessage, error) {
	operationValue, ok := inputs[pkg.Manifest.Gesture.OperationField]
	if !ok {
		return nil, fmt.Errorf("lease input Action operation field %q is missing", pkg.Manifest.Gesture.OperationField)
	}
	operation, ok := operationValue.(string)
	if !ok || (operation != "START" && operation != "RENEW" && operation != "STOP") {
		return nil, fmt.Errorf("lease input Action operation field %q must be START, RENEW, or STOP", pkg.Manifest.Gesture.OperationField)
	}
	if operation == "START" {
		return c.startLease(ctx, pkg, ruleID, selection, binding)
	}
	leaseValue, ok := inputs[pkg.Manifest.Gesture.LeaseIDField]
	if !ok {
		return nil, fmt.Errorf("lease input Action lease field %q is missing", pkg.Manifest.Gesture.LeaseIDField)
	}
	leaseID, ok := leaseValue.(string)
	if !ok || strings.TrimSpace(leaseID) == "" || strings.TrimSpace(leaseID) != leaseID {
		return nil, fmt.Errorf("lease input Action lease field %q must be a canonical string", pkg.Manifest.Gesture.LeaseIDField)
	}
	if operation == "STOP" {
		return c.stopLease(ctx, pkg, ruleID, selection, leaseID)
	}
	return c.renewLease(ctx, pkg, ruleID, selection, leaseID)
}

func (c *Controller) startLease(ctx context.Context, pkg *Package, ruleID, selection string, binding Binding) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeLease != nil {
		return nil, fmt.Errorf("key hold lease %q is already active", c.activeLease.id)
	}
	before, err := c.foreground()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground before key hold lease: %w", err)
	}
	if !strings.EqualFold(before.ExecutableName, ruleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", before.ExecutableName, ruleID)
	}
	resolved, err := c.resolveLeaseBindings(pkg.Manifest.BindingSource, binding)
	if err != nil {
		return nil, err
	}
	revalidated, err := c.resolveLeaseBindings(pkg.Manifest.BindingSource, binding)
	if err != nil {
		return nil, fmt.Errorf("revalidate key hold binding before injection: %w", err)
	}
	if !sameResolvedBindings(revalidated, resolved) {
		return nil, errors.New("key hold binding changed before injection")
	}
	current, err := c.foreground()
	if err != nil {
		return nil, fmt.Errorf("revalidate foreground before key hold injection: %w", err)
	}
	if !sameForeground(before, current) || !strings.EqualFold(current.ExecutableName, ruleID) {
		return nil, errors.New("foreground process changed before key hold injection")
	}
	leaseID, err := newLeaseID()
	if err != nil {
		return nil, err
	}
	evidence := make([]windowsinput.Evidence, 0, len(resolved))
	for _, item := range resolved {
		itemEvidence, downErr := c.driver.KeyDown(ctx, windowsinput.KeyRequest{Key: item.key})
		if downErr != nil {
			var releaseErrors []error
			for index := len(evidence) - 1; index >= 0; index-- {
				if _, releaseErr := c.driver.KeyUp(context.Background(), windowsinput.KeyRequest{Key: resolved[index].key}); releaseErr != nil {
					releaseErrors = append(releaseErrors, releaseErr)
				}
			}
			return nil, errors.Join(fmt.Errorf("start key hold lease for %s: %w", item.key, downErr), errors.Join(releaseErrors...))
		}
		evidence = append(evidence, itemEvidence)
	}
	lease := &keyLease{
		id: leaseID, ruleID: ruleID, selection: selection, resolved: resolved,
		evidence: evidence, leaseMS: pkg.Manifest.Gesture.LeaseMS, state: "ACTIVE", generation: 1,
	}
	lease.timer = time.AfterFunc(time.Duration(lease.leaseMS)*time.Millisecond, func() { c.expireLease(leaseID, 1) })
	c.activeLease = lease
	return c.encodeLeaseResult(pkg, "START", lease)
}

func (c *Controller) renewLease(ctx context.Context, pkg *Package, ruleID, selection, leaseID string) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, err := c.requireActiveLease(ruleID, selection, leaseID)
	if err != nil {
		return nil, err
	}
	current, err := c.foreground()
	if err != nil {
		return nil, fmt.Errorf("resolve foreground before key hold renewal: %w", err)
	}
	if !strings.EqualFold(current.ExecutableName, ruleID) {
		return nil, fmt.Errorf("foreground executable is %q, expected owning Rule %q", current.ExecutableName, ruleID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lease.generation++
	generation := lease.generation
	lease.timer.Stop()
	lease.timer = time.AfterFunc(time.Duration(lease.leaseMS)*time.Millisecond, func() { c.expireLease(leaseID, generation) })
	return c.encodeLeaseResult(pkg, "RENEW", lease)
}

func (c *Controller) stopLease(ctx context.Context, pkg *Package, ruleID, selection, leaseID string) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeLease == nil || c.activeLease.id != leaseID {
		if lease := c.recentReleasedLeases[leaseID]; lease != nil && strings.EqualFold(lease.ruleID, ruleID) && lease.selection == selection {
			if lease.releaseErr != nil {
				return nil, fmt.Errorf("key hold lease %q release failed: %w", leaseID, lease.releaseErr)
			}
			return c.encodeLeaseResult(pkg, "STOP", lease)
		}
		return nil, fmt.Errorf("key hold lease %q is not active", leaseID)
	}
	lease, err := c.requireActiveLease(ruleID, selection, leaseID)
	if err != nil {
		return nil, err
	}
	lease.timer.Stop()
	if err := c.releaseLeaseKeys(ctx, lease); err != nil {
		return nil, fmt.Errorf("stop key hold lease %q: %w", leaseID, err)
	}
	lease.state = "RELEASED"
	lease.releaseReason = "EXPLICIT"
	c.activeLease = nil
	c.recordReleasedLease(lease)
	return c.encodeLeaseResult(pkg, "STOP", lease)
}

func (c *Controller) recordReleasedLease(lease *keyLease) {
	if lease == nil {
		return
	}
	if _, exists := c.recentReleasedLeases[lease.id]; !exists {
		c.releasedLeaseOrder = append(c.releasedLeaseOrder, lease.id)
	}
	c.recentReleasedLeases[lease.id] = lease
	if len(c.releasedLeaseOrder) <= recentReleasedLeaseLimit {
		return
	}
	oldest := c.releasedLeaseOrder[0]
	c.releasedLeaseOrder = c.releasedLeaseOrder[1:]
	delete(c.recentReleasedLeases, oldest)
}

func (c *Controller) requireActiveLease(ruleID, selection, leaseID string) (*keyLease, error) {
	if c.activeLease == nil || c.activeLease.id != leaseID {
		return nil, fmt.Errorf("key hold lease %q is not active", leaseID)
	}
	if !strings.EqualFold(c.activeLease.ruleID, ruleID) || c.activeLease.selection != selection {
		return nil, fmt.Errorf("key hold lease %q ownership does not match this Action", leaseID)
	}
	return c.activeLease, nil
}

func (c *Controller) expireLease(leaseID string, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeLease == nil || c.activeLease.id != leaseID || c.activeLease.generation != generation {
		return
	}
	lease := c.activeLease
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := c.releaseLeaseKeys(ctx, lease)
	cancel()
	lease.state = "RELEASED"
	lease.releaseReason = "EXPIRED"
	lease.releaseErr = err
	c.activeLease = nil
	c.recordReleasedLease(lease)
}

func (c *Controller) encodeLeaseResult(pkg *Package, operation string, lease *keyLease) (json.RawMessage, error) {
	result := map[string]any{
		"schemaVersion": int64(1), "operation": operation, "selection": lease.selection,
		"bindingSource": pkg.Manifest.BindingSource.Type, "backend": lease.evidence[0].Backend, "leaseId": lease.id,
		"leaseMs": int64(lease.leaseMS), "leaseState": lease.state,
		"releaseReason": nil,
	}
	if lease.releaseReason != "" {
		result["releaseReason"] = lease.releaseReason
	}
	if len(lease.resolved) == 1 {
		result["key"] = lease.resolved[0].key
		result["scanCode"] = int64(lease.evidence[0].ScanCode)
		result["extended"] = lease.evidence[0].Extended
		if lease.resolved[0].control != "" {
			result["control"] = lease.resolved[0].control
		}
	} else {
		controls := make([]any, len(lease.resolved))
		keys := make([]any, len(lease.resolved))
		scanCodes := make([]any, len(lease.resolved))
		extended := make([]any, len(lease.resolved))
		for index := range lease.resolved {
			controls[index] = lease.resolved[index].control
			keys[index] = lease.resolved[index].key
			scanCodes[index] = int64(lease.evidence[index].ScanCode)
			extended[index] = lease.evidence[index].Extended
		}
		result["controls"] = controls
		result["keys"] = keys
		result["scanCodes"] = scanCodes
		result["extendedKeys"] = extended
	}
	if lease.resolved[0].preset != "" {
		result["activePreset"] = lease.resolved[0].preset
		result["bindingFile"] = lease.resolved[0].filename
	}
	if err := pkg.ValidateOutput(result); err != nil {
		return nil, fmt.Errorf("validate key hold Action output: %w", err)
	}
	return json.Marshal(result)
}

func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activeLease == nil {
		return nil
	}
	lease := c.activeLease
	lease.timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := c.releaseLeaseKeys(ctx, lease)
	c.activeLease = nil
	return err
}

func (c *Controller) releaseLeaseKeys(ctx context.Context, lease *keyLease) error {
	var releaseErrors []error
	for index := len(lease.resolved) - 1; index >= 0; index-- {
		if _, err := c.driver.KeyUp(ctx, windowsinput.KeyRequest{Key: lease.resolved[index].key}); err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release %s: %w", lease.resolved[index].key, err))
		}
	}
	return errors.Join(releaseErrors...)
}

func (c *Controller) resolveLeaseBindings(source BindingSource, binding Binding) ([]resolvedBinding, error) {
	if len(binding.Controls) == 0 {
		resolved, err := c.resolveBinding(source, binding)
		if err != nil {
			return nil, err
		}
		return []resolvedBinding{resolved}, nil
	}
	result := make([]resolvedBinding, 0, len(binding.Controls))
	seenKeys := map[string]struct{}{}
	for _, control := range binding.Controls {
		resolved, err := c.resolveBinding(source, Binding{Control: control})
		if err != nil {
			return nil, err
		}
		if _, exists := seenKeys[resolved.key]; exists {
			return nil, fmt.Errorf("compound key hold resolves duplicate key %s", resolved.key)
		}
		seenKeys[resolved.key] = struct{}{}
		result = append(result, resolved)
	}
	return result, nil
}

func sameResolvedBindings(left, right []resolvedBinding) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func newLeaseID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate key hold lease ID: %w", err)
	}
	return "key_" + hex.EncodeToString(buffer), nil
}

func resolveHoldMS(gesture Gesture, inputs map[string]any) (uint32, error) {
	if gesture.HoldMSInputField == "" {
		return gesture.HoldMS, nil
	}
	value, ok := inputs[gesture.HoldMSInputField]
	if !ok {
		return gesture.HoldMS, nil
	}
	var hold int64
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("input Action hold field %q must be an integer", gesture.HoldMSInputField)
		}
		hold = int64(typed)
	case int:
		hold = int64(typed)
	case int64:
		hold = typed
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, fmt.Errorf("input Action hold field %q must be an integer", gesture.HoldMSInputField)
		}
		hold = parsed
	default:
		return 0, fmt.Errorf("input Action hold field %q must be an integer", gesture.HoldMSInputField)
	}
	if hold < int64(gesture.MinHoldMS) || hold > int64(gesture.MaxHoldMS) {
		return 0, fmt.Errorf("input Action hold field %q must be from %d through %d", gesture.HoldMSInputField, gesture.MinHoldMS, gesture.MaxHoldMS)
	}
	return uint32(hold), nil
}

func selectBinding(selector Selector, inputs map[string]any) (string, error) {
	if selector.Constant != "" {
		return selector.Constant, nil
	}
	value, ok := inputs[selector.InputField]
	if !ok {
		return "", fmt.Errorf("input Action selector field %q is missing", selector.InputField)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return "", fmt.Errorf("input Action selector field %q must be an integer or string", selector.InputField)
		}
		return fmt.Sprintf("%.0f", typed), nil
	case int:
		return fmt.Sprintf("%d", typed), nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			return "", fmt.Errorf("input Action selector field %q must be an integer or string", selector.InputField)
		}
		return fmt.Sprintf("%d", integer), nil
	default:
		return "", fmt.Errorf("input Action selector field %q has unsupported type", selector.InputField)
	}
}

type resolvedBinding struct {
	key      string
	control  string
	preset   string
	filename string
}

func (c *Controller) resolveBinding(source BindingSource, binding Binding) (resolvedBinding, error) {
	if source.Type == BindingSourceLiteral {
		if _, err := windowsinput.VirtualKey(binding.Key); err != nil {
			return resolvedBinding{}, err
		}
		return resolvedBinding{key: binding.Key}, nil
	}
	if source.Type != BindingSourceFrontier {
		return resolvedBinding{}, fmt.Errorf("unsupported input Action binding source %q", source.Type)
	}
	if c.frontierBindingsRoot == "" {
		return resolvedBinding{}, errors.New("Frontier binding source is not configured")
	}
	presetBytes, err := os.ReadFile(filepath.Join(c.frontierBindingsRoot, activePresetFilename))
	if err != nil {
		return resolvedBinding{}, fmt.Errorf("read active Elite Dangerous preset: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(presetBytes), "\r\n", "\n"), "\n")
	activePreset := firstLine(lines)
	if activePreset == "" || strings.TrimSpace(activePreset) != activePreset {
		return resolvedBinding{}, errors.New("active Elite Dangerous preset is empty or non-canonical")
	}
	bindingPath, err := c.findActiveBindingFile(activePreset)
	if err != nil {
		return resolvedBinding{}, err
	}
	info, err := os.Lstat(bindingPath)
	if err != nil {
		return resolvedBinding{}, fmt.Errorf("stat Elite Dangerous binding file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return resolvedBinding{}, errors.New("Elite Dangerous binding file must be a regular non-symlink file")
	}
	if info.Size() > maxFileBytes {
		return resolvedBinding{}, errors.New("Elite Dangerous binding file exceeds the size limit")
	}
	file, err := os.Open(bindingPath)
	if err != nil {
		return resolvedBinding{}, fmt.Errorf("open Elite Dangerous binding file: %w", err)
	}
	defer file.Close()
	decoder := xml.NewDecoder(file)
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return resolvedBinding{}, fmt.Errorf("decode Elite Dangerous binding file: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != binding.Control {
			continue
		}
		var control struct {
			Primary   bindingEndpoint `xml:"Primary"`
			Secondary bindingEndpoint `xml:"Secondary"`
		}
		if err := decoder.DecodeElement(&control, &start); err != nil {
			return resolvedBinding{}, fmt.Errorf("decode Elite Dangerous control %s: %w", binding.Control, err)
		}
		keys := map[string]struct{}{}
		for _, endpoint := range []bindingEndpoint{control.Primary, control.Secondary} {
			if endpoint.Device == "Keyboard" && endpoint.Key != "" {
				keys[endpoint.Key] = struct{}{}
			}
		}
		if len(keys) == 0 {
			return resolvedBinding{}, fmt.Errorf("Elite Dangerous control %s has no Keyboard binding", binding.Control)
		}
		if len(keys) != 1 {
			return resolvedBinding{}, fmt.Errorf("Elite Dangerous control %s has ambiguous Keyboard bindings", binding.Control)
		}
		for key := range keys {
			if _, err := windowsinput.VirtualKey(key); err != nil {
				return resolvedBinding{}, fmt.Errorf("Elite Dangerous control %s: %w", binding.Control, err)
			}
			return resolvedBinding{key: key, control: binding.Control, preset: activePreset, filename: filepath.Base(bindingPath)}, nil
		}
	}
	return resolvedBinding{}, fmt.Errorf("Elite Dangerous control %s is missing from active preset %s", binding.Control, activePreset)
}

func (c *Controller) findActiveBindingFile(activePreset string) (string, error) {
	entries, err := os.ReadDir(c.frontierBindingsRoot)
	if err != nil {
		return "", fmt.Errorf("list Elite Dangerous binding files: %w", err)
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".binds") {
			continue
		}
		name := filepath.Join(c.frontierBindingsRoot, entry.Name())
		presetName, err := readPresetName(name)
		if err != nil {
			return "", fmt.Errorf("inspect Elite Dangerous binding file %s: %w", entry.Name(), err)
		}
		if presetName == activePreset {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no .binds file declares active Elite Dangerous preset %q", activePreset)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("multiple .binds files declare active Elite Dangerous preset %q", activePreset)
	}
	return matches[0], nil
}

func readPresetName(name string) (string, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxFileBytes {
		return "", errors.New("binding file must be one bounded regular non-symlink file")
	}
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	decoder := xml.NewDecoder(file)
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "Root" {
			return "", errors.New("first XML element is not Root")
		}
		for _, attribute := range start.Attr {
			if attribute.Name.Local == "PresetName" && attribute.Value != "" {
				return attribute.Value, nil
			}
		}
		return "", errors.New("Root is missing PresetName")
	}
}

type bindingEndpoint struct {
	Device string `xml:"Device,attr"`
	Key    string `xml:"Key,attr"`
}

func firstLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func sameForeground(left, right foreground.Info) bool {
	return left.ProcessID == right.ProcessID && strings.EqualFold(left.ExecutableName, right.ExecutableName) &&
		strings.EqualFold(filepath.Clean(left.ExecutablePath), filepath.Clean(right.ExecutablePath))
}
