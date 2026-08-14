package actionsequence

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Candidate is one Rule-approved Action exposed to the model-facing tool.
type Candidate struct {
	ID          string
	Description string
	InputSchema json.RawMessage
}

// ToolSchema is a strict function-tool declaration for run_action_sequence.
type ToolSchema struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Strict      bool           `json:"strict"`
	Parameters  map[string]any `json:"parameters"`
}

func BuildToolSchema(ruleID string, candidates []Candidate) (ToolSchema, error) {
	if ruleID == "" {
		return ToolSchema{}, errors.New("Action Sequence tool schema requires a Rule ID")
	}
	if len(candidates) == 0 {
		return ToolSchema{}, errors.New("Action Sequence requires at least one allowed Action")
	}
	ordered := append([]Candidate(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	branches := make([]any, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for _, candidate := range ordered {
		if candidate.ID == "" || candidate.Description == "" || len(candidate.InputSchema) == 0 {
			return ToolSchema{}, errors.New("Action Sequence candidate requires ID, description, and input schema")
		}
		if _, exists := seen[candidate.ID]; exists {
			return ToolSchema{}, fmt.Errorf("Action Sequence candidate %q is duplicated", candidate.ID)
		}
		seen[candidate.ID] = struct{}{}
		var input any
		if err := json.Unmarshal(candidate.InputSchema, &input); err != nil {
			return ToolSchema{}, fmt.Errorf("decode Action %s input schema: %w", candidate.ID, err)
		}
		normalized, err := strictModelSchema(input)
		if err != nil {
			return ToolSchema{}, fmt.Errorf("normalize Action %s input schema: %w", candidate.ID, err)
		}
		normalizedObject, ok := normalized.(map[string]any)
		if !ok || normalizedObject["type"] != "object" {
			return ToolSchema{}, fmt.Errorf("Action %s input schema must have object type", candidate.ID)
		}
		branches = append(branches, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string", "enum": []any{candidate.ID}, "description": candidate.Description,
				},
				"inputs": normalizedObject,
			},
			"required":             []any{"action", "inputs"},
			"additionalProperties": false,
		})
	}
	return ToolSchema{
		Type: "function",
		Name: "run_action_sequence",
		Description: "Execute one immutable ephemeral sequence of 1 to 20 existing Actions strictly in order. " +
			"All steps are validated before execution. The sequence has no branches, loops, nesting, variables, or output references; " +
			"use null for an optional Action input to preserve its ordinary omission/default semantics. " +
			"it fails on the first child failure and can be cancelled through its invocation stop target.",
		Strict: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ruleId": map[string]any{"type": "string", "enum": []any{ruleID}},
				"steps": map[string]any{
					"type": "array", "minItems": float64(1), "maxItems": float64(MaxSteps), "items": map[string]any{"anyOf": branches},
				},
			},
			"required":             []any{"ruleId", "steps"},
			"additionalProperties": false,
		},
	}, nil
}

func strictModelSchema(value any) (any, error) {
	switch current := value.(type) {
	case []any:
		result := make([]any, len(current))
		for index, item := range current {
			normalized, err := strictModelSchema(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case map[string]any:
		originalRequired := stringSet(current["required"])
		result := make(map[string]any, len(current))
		for key, item := range current {
			switch key {
			case "$schema", "$id", "default", "examples":
				continue
			case "oneOf":
				if _, exists := current["anyOf"]; exists {
					return nil, errors.New("schema cannot declare both oneOf and anyOf")
				}
				key = "anyOf"
			}
			normalized, err := strictModelSchema(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		if defaultValue, exists := current["default"]; exists {
			encodedDefault, err := json.Marshal(defaultValue)
			if err != nil {
				return nil, fmt.Errorf("encode Action input default: %w", err)
			}
			description, _ := result["description"].(string)
			if description != "" {
				description += " "
			}
			result["description"] = description + "Action default: " + string(encodedDefault) + "."
		}
		if result["type"] == "object" {
			properties, ok := result["properties"].(map[string]any)
			if !ok {
				properties = map[string]any{}
				result["properties"] = properties
			}
			keys := make([]string, 0, len(properties))
			for key := range properties {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if _, required := originalRequired[key]; required {
					continue
				}
				properties[key] = map[string]any{
					"anyOf": []any{properties[key], map[string]any{"type": "null"}},
				}
			}
			required := make([]any, len(keys))
			for index, key := range keys {
				required[index] = key
			}
			result["required"] = required
			result["additionalProperties"] = false
		}
		return result, nil
	default:
		return value, nil
	}
}

// CanonicalInputs translates the strict model-tool representation back to the
// Action's canonical input contract. Strict function tools require every
// property to be present, so a null value means "omit this optional property"
// only when the Action schema itself does not accept null.
func CanonicalInputs(inputSchema json.RawMessage, inputs map[string]any) (map[string]any, error) {
	var schema map[string]any
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode Action input schema: %w", err)
	}
	canonical, err := canonicalInputValue(schema, inputs)
	if err != nil {
		return nil, err
	}
	result, ok := canonical.(map[string]any)
	if !ok {
		return nil, errors.New("Action input schema must have object type")
	}
	return result, nil
}

func canonicalInputValue(schema map[string]any, value any) (any, error) {
	if schema["type"] != "object" {
		return value, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}
	properties, _ := schema["properties"].(map[string]any)
	required := stringSet(schema["required"])
	result := make(map[string]any, len(object))
	for key, item := range object {
		property, declared := properties[key].(map[string]any)
		_, isRequired := required[key]
		if item == nil && declared && !isRequired && !schemaAcceptsNull(property) {
			continue
		}
		if declared {
			canonical, err := canonicalInputValue(property, item)
			if err != nil {
				return nil, err
			}
			result[key] = canonical
			continue
		}
		result[key] = item
	}
	return result, nil
}

func stringSet(value any) map[string]struct{} {
	result := map[string]struct{}{}
	items, _ := value.([]any)
	for _, item := range items {
		if text, ok := item.(string); ok {
			result[text] = struct{}{}
		}
	}
	return result
}

func schemaAcceptsNull(schema map[string]any) bool {
	switch typed := schema["type"].(type) {
	case string:
		return typed == "null"
	case []any:
		for _, item := range typed {
			if item == "null" {
				return true
			}
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if object, ok := branch.(map[string]any); ok && schemaAcceptsNull(object) {
				return true
			}
		}
	}
	return false
}
