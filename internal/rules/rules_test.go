package rules

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreResolvesLiveRuleAndReadsAGENTS(t *testing.T) {
	root := testRulesRoot(t)
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := store.Resolve("CRIMSONDESERT.EXE")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != StatusMatched || resolution.ID != "CrimsonDesert.exe" ||
		resolution.Description != "Read the live Rule before acting." {
		t.Fatalf("resolution = %+v", resolution)
	}
	if resolution.Agents == nil ||
		resolution.Agents.URL != "/v1/rules/CrimsonDesert.exe/AGENTS.md" {
		t.Fatalf("AGENTS navigation = %+v", resolution.Agents)
	}
	content, readResolution, err := store.ReadAGENTS("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Guidance\n" || readResolution.Description != resolution.Description {
		t.Fatal("read AGENTS content or description differs from resolution")
	}
	if err := resolution.Validate(); err != nil {
		t.Fatalf("matched resolution rejected: %v", err)
	}

	writeRule(t, root, "CrimsonDesert.exe", "Updated without reload.", "# Updated\n", nil)
	updated, err := store.Resolve("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	updatedContent, _, err := store.ReadAGENTS("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != "Updated without reload." || string(updatedContent) != "# Updated\n" {
		t.Fatalf("live update was not observed: %+v %q", updated, updatedContent)
	}
}

func TestStoreReportsUnmatchedExplicitly(t *testing.T) {
	store, err := New(testRulesRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := store.Resolve("explorer.exe")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != StatusUnmatched ||
		resolution.Description != UnmatchedDescription ||
		resolution.ID != "" ||
		resolution.Agents != nil {
		t.Fatalf("resolution = %+v", resolution)
	}
	if err := resolution.Validate(); err != nil {
		t.Fatalf("unmatched resolution rejected: %v", err)
	}
}

func TestStoreResolvesRegisteredScript(t *testing.T) {
	root := testRulesRoot(t)
	scriptRoot := filepath.Join(root, "CrimsonDesert.exe", "Scripts", "inventory")
	if err := os.MkdirAll(scriptRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	script, err := store.ResolveScript("crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	canonicalScriptRoot, err := filepath.EvalSymlinks(scriptRoot)
	if err != nil {
		t.Fatal(err)
	}
	if script.RuleID != "CrimsonDesert.exe" || script.Runtime != ObservationRuntimeV1 ||
		script.Root != canonicalScriptRoot {
		t.Fatalf("script = %+v", script)
	}
}

func TestRepositoryCrimsonRulePluginResolves(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := store.Resolve("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != StatusMatched || resolution.Description == "" {
		t.Fatalf("resolution = %+v", resolution)
	}
	script, err := store.ResolveScript("crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	if script.Runtime != ObservationRuntimeV1 {
		t.Fatalf("script runtime = %q", script.Runtime)
	}
}

func TestStoreRejectsInvalidRuleDocuments(t *testing.T) {
	tests := []struct {
		name       string
		ruleJSON   string
		agents     string
		want       string
		scriptRoot bool
	}{
		{name: "wrong schema", ruleJSON: `{"schemaVersion":2,"description":"Valid.","scripts":{}}`, agents: "# A\n", want: "schemaVersion"},
		{name: "empty description", ruleJSON: `{"schemaVersion":1,"description":"","scripts":{}}`, agents: "# A\n", want: "description"},
		{name: "missing scripts", ruleJSON: `{"schemaVersion":1,"description":"Valid."}`, agents: "# A\n", want: "scripts is required"},
		{name: "unknown field", ruleJSON: `{"schemaVersion":1,"description":"Valid.","scripts":{},"extra":true}`, agents: "# A\n", want: "unknown field"},
		{name: "duplicate key", ruleJSON: `{"schemaVersion":1,"description":"One.","description":"Two.","scripts":{}}`, agents: "# A\n", want: "duplicate"},
		{name: "empty agents", ruleJSON: `{"schemaVersion":1,"description":"Valid.","scripts":{}}`, agents: " \n", want: "AGENTS.md is empty"},
		{
			name:     "path traversal",
			ruleJSON: `{"schemaVersion":1,"description":"Valid.","scripts":{"bad":{"path":"Scripts/../bad","runtime":"windows-observation-v1"}}}`,
			agents:   "# A\n",
			want:     "not canonical",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			ruleRoot := filepath.Join(root, "game.exe")
			if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ruleRoot, RuleFilename), []byte(test.ruleJSON), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ruleRoot, AgentsFilename), []byte(test.agents), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Resolve("game.exe"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestResolveScriptRejectsDuplicateCapability(t *testing.T) {
	root := testRulesRoot(t)
	writeRule(t, root, "other.exe", "Other.", "# Other\n", map[string]ScriptDeclaration{
		"crimson-desert/inventory": {Path: "Scripts/inventory", Runtime: ObservationRuntimeV1},
	})
	for _, id := range []string{"CrimsonDesert.exe", "other.exe"} {
		if err := os.MkdirAll(filepath.Join(root, id, "Scripts", "inventory"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveScript("crimson-desert/inventory"); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("ResolveScript() error = %v", err)
	}
}

func TestResolveScriptRejectsMissingDirectory(t *testing.T) {
	store, err := New(testRulesRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveScript("crimson-desert/inventory"); err == nil {
		t.Fatal("ResolveScript accepted a missing Script directory")
	}
}

func TestResolveRejectsRuleMemberSymlinkEscape(t *testing.T) {
	root := testRulesRoot(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "CrimsonDesert.exe", AgentsFilename)
	if err := os.Remove(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, agents); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve("CrimsonDesert.exe"); err == nil ||
		!strings.Contains(err.Error(), "outside") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestReadAGENTSRejectsUnknownOrNonCanonicalID(t *testing.T) {
	store, err := New(testRulesRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unknown.exe", "crimsondesert.exe"} {
		if _, _, err := store.ReadAGENTS(id); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ReadAGENTS(%q) error = %v, want fs.ErrNotExist", id, err)
		}
	}
}

func testRulesRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRule(t, root, "CrimsonDesert.exe", "Read the live Rule before acting.", "# Guidance\n", map[string]ScriptDeclaration{
		"crimson-desert/inventory": {Path: "Scripts/inventory", Runtime: ObservationRuntimeV1},
	})
	return root
}

func writeRule(t *testing.T, root, id, description, agents string, scripts map[string]ScriptDeclaration) {
	t.Helper()
	if scripts == nil {
		scripts = map[string]ScriptDeclaration{}
	}
	ruleRoot := filepath.Join(root, id)
	if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := Descriptor{SchemaVersion: 1, Description: description, Scripts: scripts}
	encoded, err := jsonMarshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleRoot, RuleFilename), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleRoot, AgentsFilename), []byte(agents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jsonMarshal(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
