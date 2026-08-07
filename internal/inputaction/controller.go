package inputaction

import (
	"context"
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

type Controller struct {
	frontierBindingsRoot string
	driver               windowsinput.Driver
	foreground           func() (foreground.Info, error)
	mu                   sync.Mutex
}

func NewController(frontierBindingsRoot string, driver windowsinput.Driver, foregroundSnapshot func() (foreground.Info, error)) (*Controller, error) {
	if frontierBindingsRoot != "" && !filepath.IsAbs(frontierBindingsRoot) {
		return nil, errors.New("configured Frontier bindings root must be absolute")
	}
	if driver == nil || foregroundSnapshot == nil {
		return nil, errors.New("Windows input driver and foreground resolver are required")
	}
	return &Controller{frontierBindingsRoot: frontierBindingsRoot, driver: driver, foreground: foregroundSnapshot}, nil
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
	current, err := c.foreground()
	if err != nil {
		return nil, fmt.Errorf("revalidate foreground before input injection: %w", err)
	}
	if !sameForeground(before, current) || !strings.EqualFold(current.ExecutableName, ruleID) {
		return nil, errors.New("foreground process changed before input injection")
	}
	evidence, err := c.driver.Press(ctx, windowsinput.PressRequest{
		Key: resolved.key, Hold: time.Duration(pkg.Manifest.Gesture.HoldMS) * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("press input Action key %s: %w", resolved.key, err)
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
