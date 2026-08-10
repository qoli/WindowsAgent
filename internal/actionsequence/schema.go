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
