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
	if resolution.Scripts == nil ||
		resolution.Scripts.URL != "/v1/rules/CrimsonDesert.exe/scripts" {
		t.Fatalf("Scripts navigation = %+v", resolution.Scripts)
	}
	if resolution.Modules == nil ||
		resolution.Modules.URL != "/v2/rules/CrimsonDesert.exe/modules" {
		t.Fatalf("Modules navigation = %+v", resolution.Modules)
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
		resolution.Agents != nil ||
		resolution.Scripts != nil ||
		resolution.Modules != nil {
		t.Fatalf("resolution = %+v", resolution)
	}
	if err := resolution.Validate(); err != nil {
		t.Fatalf("unmatched resolution rejected: %v", err)
	}
}

func TestStoreResolvesRegisteredScript(t *testing.T) {
	root := testRulesRoot(t)
	scriptRoot := filepath.Join(root, "CrimsonDesert.exe", "Modules", "inventory")
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

func TestStoreReadsRuleScriptsInCanonicalOrder(t *testing.T) {
	root := testRulesRoot(t)
	writeRule(t, root, "CrimsonDesert.exe", "Read the live Rule before acting.", "# Guidance\n", map[string]ModuleDeclaration{
		"zeta/capability":          {Kind: ModuleKindQuery, Path: "Modules/zeta", Runtime: ObservationRuntimeV1},
		"crimson-desert/inventory": {Kind: ModuleKindQuery, Path: "Modules/inventory", Runtime: ObservationRuntimeV1},
	})
	for _, name := range []string{"inventory", "zeta"} {
		if err := os.MkdirAll(filepath.Join(root, "CrimsonDesert.exe", "Modules", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	scripts, resolution, err := store.ReadScripts("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 2 ||
		scripts[0].ID != "crimson-desert/inventory" ||
		scripts[1].ID != "zeta/capability" {
		t.Fatalf("scripts = %+v", scripts)
	}
	if resolution.Scripts == nil ||
		resolution.Scripts.ContentType != ScriptsMediaType {
		t.Fatalf("resolution = %+v", resolution)
	}
}

func TestStoreReadsClassifiedModulesInCanonicalOrder(t *testing.T) {
	root := testRulesRoot(t)
	writeRule(t, root, "CrimsonDesert.exe", "Read the live Rule before acting.", "# Guidance\n", map[string]ModuleDeclaration{
		"screen/ui":     {Kind: ModuleKindLoop, Path: "Modules/screen-ui", Runtime: "screenparser-v2"},
		"reaction/fast": {Kind: ModuleKindReactor, Path: "Reactors/fast", Runtime: "local-mini-model-v1"},
		"action/dock":   {Kind: ModuleKindAction, Path: "Actions/dock", Runtime: "windows-action-v1"},
	})
	for _, path := range []string{"Modules/screen-ui", "Reactors/fast", "Actions/dock"} {
		if err := os.MkdirAll(filepath.Join(root, "CrimsonDesert.exe", filepath.FromSlash(path)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	modules, resolution, err := store.ReadModules("CrimsonDesert.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 3 || modules[0].ID != "action/dock" || modules[1].ID != "reaction/fast" || modules[2].ID != "screen/ui" {
		t.Fatalf("modules = %+v", modules)
	}
	if resolution.Modules == nil || resolution.Modules.ContentType != ModulesMediaType {
		t.Fatalf("resolution = %+v", resolution)
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

func TestRepositoryPalworldRuleRegistersOnDemandPreprocessor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	modules, _, err := store.ReadModules("Palworld-Win64-Shipping.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 1 || modules[0].ID != "screenparser/ui-elements" ||
		modules[0].Kind != ModuleKindPreprocessor || modules[0].Runtime != "screenparser-onnx-dml-v1" {
		t.Fatalf("Palworld modules = %+v", modules)
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
		{name: "wrong schema", ruleJSON: `{"schemaVersion":1,"description":"Valid.","modules":{}}`, agents: "# A\n", want: "schemaVersion"},
		{name: "empty description", ruleJSON: `{"schemaVersion":2,"description":"","modules":{}}`, agents: "# A\n", want: "description"},
		{name: "missing modules", ruleJSON: `{"schemaVersion":2,"description":"Valid."}`, agents: "# A\n", want: "modules is required"},
		{name: "unknown field", ruleJSON: `{"schemaVersion":2,"description":"Valid.","modules":{},"extra":true}`, agents: "# A\n", want: "unknown field"},
		{name: "duplicate key", ruleJSON: `{"schemaVersion":2,"description":"One.","description":"Two.","modules":{}}`, agents: "# A\n", want: "duplicate"},
		{name: "empty agents", ruleJSON: `{"schemaVersion":2,"description":"Valid.","modules":{}}`, agents: " \n", want: "AGENTS.md is empty"},
		{
			name:     "path traversal",
			ruleJSON: `{"schemaVersion":2,"description":"Valid.","modules":{"bad":{"kind":"query","path":"Modules/../bad","runtime":"windows-observation-v1"}}}`,
			agents:   "# A\n",
			want:     "not canonical",
		},
		{
			name:     "unknown module kind",
			ruleJSON: `{"schemaVersion":2,"description":"Valid.","modules":{"bad":{"kind":"background","path":"Modules/bad","runtime":"windows-script-v2"}}}`,
			agents:   "# A\n",
			want:     "unsupported module kind",
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
	writeRule(t, root, "other.exe", "Other.", "# Other\n", map[string]ModuleDeclaration{
		"crimson-desert/inventory": {Kind: ModuleKindQuery, Path: "Modules/inventory", Runtime: ObservationRuntimeV1},
	})
	for _, id := range []string{"CrimsonDesert.exe", "other.exe"} {
		if err := os.MkdirAll(filepath.Join(root, id, "Modules", "inventory"), 0o700); err != nil {
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
	writeRule(t, root, "CrimsonDesert.exe", "Read the live Rule before acting.", "# Guidance\n", map[string]ModuleDeclaration{
		"crimson-desert/inventory": {Kind: ModuleKindQuery, Path: "Modules/inventory", Runtime: ObservationRuntimeV1},
	})
	return root
}

func writeRule(t *testing.T, root, id, description, agents string, modules map[string]ModuleDeclaration) {
	t.Helper()
	if modules == nil {
		modules = map[string]ModuleDeclaration{}
	}
	ruleRoot := filepath.Join(root, id)
	if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := Descriptor{SchemaVersion: 2, Description: description, Modules: modules}
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
