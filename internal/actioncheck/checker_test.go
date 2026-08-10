package actioncheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/rules"
)

type fixtureAction struct {
	id         string
	path       string
	runtime    string
	completion string
	script     string
}

func TestCheckAcceptsStaticDependenciesInsideWhile(t *testing.T) {
	root := t.TempDir()
	writeFixtureRule(t, root, "Game.exe", []fixtureAction{
		{
			id: "game/workflow", path: "Actions/workflow", runtime: rules.CompositeActionRuntimeV1,
			completion: rules.CompletionReturn,
			script: `def main(ctx):
    while True:
        action.try_call(id="game/status", inputs={})
        break
    action.on_failure(id="game/stop", inputs={})
    action.clear_on_failure()
    return {"ok": True}
`,
		},
		{id: "game/status", path: "Actions/status", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn},
		{id: "game/stop", path: "Actions/stop", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn},
	})

	result, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || len(result.Issues) != 0 || result.DependencyCount != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckReportsDependencyFailures(t *testing.T) {
	tests := []struct {
		name         string
		rules        map[string][]fixtureAction
		expectedCode string
	}{
		{
			name: "missing",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`"game/missing"`)},
			}},
			expectedCode: CodeMissingAction,
		},
		{
			name: "dynamic",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`ctx.inputs["action"]`)},
			}},
			expectedCode: CodeDynamicActionID,
		},
		{
			name: "indirect alias",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: "def main(ctx):\n    invoke = action.call\n    invoke(id=\"game/child\", inputs={})\n    return {\"ok\": True}\n"},
				{id: "game/child", path: "Actions/child", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn},
			}},
			expectedCode: CodeIndirectActionUse,
		},
		{
			name: "self",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`"game/parent"`)},
			}},
			expectedCode: CodeSelfDependency,
		},
		{
			name: "streaming child",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`"game/child"`)},
				{id: "game/child", path: "Actions/child", runtime: rules.StreamingActionRuntimeV1, completion: rules.CompletionStream},
			}},
			expectedCode: CodeStreamingChild,
		},
		{
			name: "cross Rule",
			rules: map[string][]fixtureAction{
				"GameA.exe": {
					{id: "game-a/parent", path: "Actions/parent", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
						script: callScript(`"game-b/child"`)},
				},
				"GameB.exe": {
					{id: "game-b/child", path: "Actions/child", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn},
				},
			},
			expectedCode: CodeCrossRuleAction,
		},
		{
			name: "cycle",
			rules: map[string][]fixtureAction{"Game.exe": {
				{id: "game/a", path: "Actions/a", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`"game/b"`)},
				{id: "game/b", path: "Actions/b", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
					script: callScript(`"game/a"`)},
			}},
			expectedCode: CodeDependencyCycle,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for ruleID, actions := range test.rules {
				writeFixtureRule(t, root, ruleID, actions)
			}
			result, err := Check(root)
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid {
				t.Fatalf("result unexpectedly valid: %+v", result)
			}
			if !hasIssueCode(result.Issues, test.expectedCode) {
				t.Fatalf("issues = %+v, expected %s", result.Issues, test.expectedCode)
			}
		})
	}
}

func TestCheckReportsInvalidPackageAndStarlark(t *testing.T) {
	root := t.TempDir()
	writeFixtureRule(t, root, "Game.exe", []fixtureAction{
		{id: "game/bad-package", path: "Actions/bad-package", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn},
		{id: "game/bad-script", path: "Actions/bad-script", runtime: rules.CompositeActionRuntimeV1, completion: rules.CompletionReturn,
			script: "def main(ctx):\n    return unknown_name\n"},
	})
	if err := os.Remove(filepath.Join(root, "Game.exe", "Actions", "bad-package", "output.schema.json")); err != nil {
		t.Fatal(err)
	}

	result, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasIssueCode(result.Issues, CodePackageLoadFailed) || !hasIssueCode(result.Issues, CodeStarlarkInvalid) {
		t.Fatalf("issues = %+v", result.Issues)
	}
}

func TestWriteTextIncludesLocationAndDependency(t *testing.T) {
	result := Result{
		SchemaVersion: 1,
		Issues: []Issue{{
			Code: CodeMissingAction, Path: "Game.exe/Actions/a/main.star", Line: 3, Column: 20,
			ActionID: "game/a", Primitive: "action.call", Dependency: "game/b", Message: "missing",
		}},
	}
	var output strings.Builder
	if err := WriteText(&output, result); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"failed: 1 issues", "main.star:3:20", CodeMissingAction, "dependency=game/b"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output = %q, missing %q", output.String(), expected)
		}
	}
}

func callScript(actionIDExpression string) string {
	return "def main(ctx):\n    action.call(id=" + actionIDExpression + ", inputs={})\n    return {\"ok\": True}\n"
}

func hasIssueCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func writeFixtureRule(t *testing.T, root, ruleID string, actions []fixtureAction) {
	t.Helper()
	ruleRoot := filepath.Join(root, ruleID)
	if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(ruleRoot, rules.AgentsFilename), "# Fixture\n")
	declarations := make(map[string]any, len(actions))
	for _, action := range actions {
		execution := map[string]any{"completion": action.completion}
		if action.completion == rules.CompletionStream {
			execution["lifecycle"] = rules.LifecycleLinear
			execution["interruptible"] = true
		}
		declarations[action.id] = map[string]any{
			"path": action.path, "runtime": action.runtime, "execution": execution, "registrableAs": []string{},
		}
		writeFixtureActionPackage(t, filepath.Join(ruleRoot, filepath.FromSlash(action.path)), action.script)
	}
	descriptor := map[string]any{
		"schemaVersion": 6, "description": "Fixture.", "runtimeProfiles": map[string]any{},
		"actions": declarations, "ephemeralActionSequence": map[string]any{"allowedActions": []string{}}, "registrations": map[string]any{},
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(ruleRoot, rules.RuleFilename), string(encoded))
}

func writeFixtureActionPackage(t *testing.T, root, script string) {
	t.Helper()
	if script == "" {
		script = "def main(ctx):\n    return {\"ok\": True}\n"
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.star":          script,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  `{"type":"object"}`,
		"output.schema.json": `{"type":"object"}`,
		"event.schema.json":  `{"type":"object"}`,
		"manifest.json": `{"schemaVersion":1,"version":1,"title":"Fixture","entrypoint":"main.star",` +
			`"taskDocument":"TASK.md","inputSchema":"input.schema.json","outputSchema":"output.schema.json",` +
			`"eventSchema":"event.schema.json","files":["main.star","TASK.md","input.schema.json","output.schema.json","event.schema.json"],` +
			`"limits":{"maxSteps":10000,"maxOutputBytes":4096,"maxEventBytes":4096,"maxSleepMs":1000}}`,
	}
	for name, content := range files {
		writeFixtureFile(t, filepath.Join(root, name), content)
	}
}

func writeFixtureFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
