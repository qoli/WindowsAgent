package actionsequence

import (
	"encoding/json"
	"testing"
)

func TestBuildToolSchemaRequiresExplicitActionInputs(t *testing.T) {
	schema, err := BuildToolSchema("Game.exe", []Candidate{
		{
			ID: "game/align", Description: "Align one static target.",
			InputSchema: json.RawMessage(`{
			  "$schema":"https://json-schema.org/draft/2020-12/schema",
			  "type":"object",
			  "properties":{"mode":{"enum":["ALIGN","TRACK"],"default":"ALIGN"}},
			  "additionalProperties":false
			}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if schema.Name != "run_action_sequence" || !schema.Strict {
		t.Fatalf("schema = %+v", schema)
	}
	text := string(encoded)
	for _, required := range []string{`"maxItems":20`, `"ruleId"`, `"Game.exe"`, `"game/align"`, `"required":["mode"]`} {
		if !contains(text, required) {
			t.Fatalf("tool schema missing %s: %s", required, text)
		}
	}
	for _, forbidden := range []string{`"$schema"`, `"default"`} {
		if contains(text, forbidden) {
			t.Fatalf("tool schema retained %s: %s", forbidden, text)
		}
	}
}

func TestBuildToolSchemaRejectsEmptyAndDuplicateCandidates(t *testing.T) {
	if _, err := BuildToolSchema("Game.exe", nil); err == nil {
		t.Fatal("empty candidate set was accepted")
	}
	candidate := Candidate{ID: "game/read", Description: "Read.", InputSchema: json.RawMessage(`{"type":"object"}`)}
	if _, err := BuildToolSchema("Game.exe", []Candidate{candidate, candidate}); err == nil {
		t.Fatal("duplicate candidate was accepted")
	}
	nonObject := Candidate{ID: "game/list", Description: "List.", InputSchema: json.RawMessage(`{"type":"array"}`)}
	if _, err := BuildToolSchema("Game.exe", []Candidate{nonObject}); err == nil {
		t.Fatal("non-object Action inputs were accepted")
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
