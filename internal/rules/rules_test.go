package rules

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStoreResolvesLiveRuleAndDocuments(t *testing.T) {
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
		resolution.Actions == nil || resolution.Actions.URL != "/v3/rules/CrimsonDesert.exe/actions" ||
		resolution.Registrations == nil || resolution.Registrations.URL != "/v3/rules/CrimsonDesert.exe/registrations" {
		t.Fatalf("resolution = %+v", resolution)
	}
	if err := resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	writeRule(t, root, "CrimsonDesert.exe", "Updated without reload.", "# Updated\n", nil, nil)
	updated, err := store.Resolve("CrimsonDesert.exe")
	if err != nil || updated.Description != "Updated without reload." {
		t.Fatalf("updated = %+v, err = %v", updated, err)
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
	if resolution.Status != StatusUnmatched || resolution.ID != "" || resolution.Actions != nil || resolution.Registrations != nil {
		t.Fatalf("resolution = %+v", resolution)
	}
	if err := resolution.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreReadsActionsAndExplicitRegistrations(t *testing.T) {
	root := t.TempDir()
	actions := map[string]ActionDeclaration{
		"game/read": {Path: "Actions/read", Runtime: ObservationRuntimeV1, Execution: returnExecution(), RegistrableAs: []string{RegistrationMonitor, RegistrationReaction}},
		"game/open": {Path: "Actions/open", Runtime: "windows-action-v1", Execution: returnExecution(), RegistrableAs: []string{RegistrationReaction}},
	}
	registrations := map[string]RegistrationDeclaration{
		"game/read-fast": {
			Type: RegistrationMonitor, Action: "game/read", Input: json.RawMessage(`{}`),
			Monitor: &MonitorTrigger{IntervalMs: 2000, Emit: EventTarget{Stream: "game.test", EventType: "game.status"}},
		},
		"game/open-after-ready": {
			Type: RegistrationReaction, Action: "game/open", Input: json.RawMessage(`{"source":"${event.eventId}"}`),
			Reaction: &ReactionTrigger{Stream: "game.test", EventType: "game.ready", Match: map[string]string{"payload.state": "^ready$"}},
		},
	}
	writeRule(t, root, "Game.exe", "Actions and registrations.", "# Game\n", actions, registrations)
	for _, name := range []string{"read", "open"} {
		if err := os.MkdirAll(filepath.Join(root, "Game.exe", "Actions", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	available, _, err := store.ReadActions("Game.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 2 || available[0].ID != "game/open" || available[1].ID != "game/read" {
		t.Fatalf("actions = %+v", available)
	}
	registered, _, err := store.ReadRegistrations("Game.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != 2 || registered[0].Type != RegistrationReaction || registered[1].Type != RegistrationMonitor {
		t.Fatalf("registrations = %+v", registered)
	}
}

func TestStoreProjectsObservationActionsToV1Scripts(t *testing.T) {
	root := testRulesRoot(t)
	actionRoot := filepath.Join(root, "CrimsonDesert.exe", "Actions", "inventory")
	if err := os.MkdirAll(actionRoot, 0o700); err != nil {
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
	canonicalActionRoot, err := filepath.EvalSymlinks(actionRoot)
	if err != nil {
		t.Fatal(err)
	}
	if script.Root != canonicalActionRoot || script.Runtime != ObservationRuntimeV1 {
		t.Fatalf("script = %+v", script)
	}
}

func TestRepositoryRulesUseActionModelWithoutDefaultRegistrations(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		rule, action, runtime string
	}{
		{"CrimsonDesert.exe", "crimson-desert/inventory", ObservationRuntimeV1},
		{"Palworld-Win64-Shipping.exe", "screenparser/ui-elements", "screenparser-onnx-dml-v1"},
	} {
		actions, _, err := store.ReadActions(test.rule)
		if err != nil {
			t.Fatal(err)
		}
		if len(actions) != 1 || actions[0].ID != test.action || actions[0].Runtime != test.runtime ||
			actions[0].Execution.Completion != CompletionReturn || len(actions[0].RegistrableAs) != 2 {
			t.Fatalf("%s actions = %+v", test.rule, actions)
		}
		registrations, _, err := store.ReadRegistrations(test.rule)
		if err != nil || len(registrations) != 0 {
			t.Fatalf("%s registrations = %+v, err = %v", test.rule, registrations, err)
		}
	}
}

func TestEliteRuleDeclaresResidentW480RuntimeAndFiniteActions(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	profiles, _, err := store.ReadRuntimeProfiles("EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[0].ID != "ocr/text-regions" ||
		profiles[0].Runtime != PpOcrTextRegionsWorkerRuntimeV1 || profiles[0].Residency != ResidencyRuleActive ||
		profiles[0].ArtifactID != "ppocrv6-small-det-onnx-official" ||
		profiles[1].ID != "ocr/w480" || profiles[1].Runtime != PpOcrWorkerRuntimeV1 ||
		profiles[1].Residency != ResidencyRuleActive || profiles[1].ArtifactID != "ppocrv6-small-rec-onnx-official-w480" {
		t.Fatalf("profiles = %+v", profiles)
	}
	action, err := store.ResolveAction("elite-dangerous/flight-prompt-text")
	if err != nil {
		t.Fatal(err)
	}
	if action.Runtime != PpOcrActionRuntimeV1 || action.RuntimeProfile != "ocr/w480" || action.Execution.Completion != CompletionReturn {
		t.Fatalf("action = %+v", action)
	}
	flightStatus, err := store.ResolveAction("elite-dangerous/flight-status")
	if err != nil {
		t.Fatal(err)
	}
	if flightStatus.Runtime != ObservationRuntimeV1 || !reflect.DeepEqual(flightStatus.RegistrableAs, []string{RegistrationMonitor, RegistrationReaction}) {
		t.Fatalf("flight-status action = %+v", flightStatus)
	}
	shipStatus, err := store.ResolveAction("elite-dangerous/ship-status")
	if err != nil {
		t.Fatal(err)
	}
	if shipStatus.Runtime != CompositeActionRuntimeV1 || !reflect.DeepEqual(shipStatus.RegistrableAs, []string{RegistrationMonitor, RegistrationReaction}) {
		t.Fatalf("ship-status action = %+v", shipStatus)
	}
	shipSpeed, err := store.ResolveAction("elite-dangerous/ship-speed")
	if err != nil {
		t.Fatal(err)
	}
	if shipSpeed.Runtime != CompositeActionRuntimeV1 || !reflect.DeepEqual(shipSpeed.RegistrableAs, []string{RegistrationMonitor, RegistrationReaction}) {
		t.Fatalf("ship-speed action = %+v", shipSpeed)
	}
	if shipSpeed.SequenceEligible {
		t.Fatal("ship-speed unexpectedly allowed in ephemeral sequences")
	}
	setThrottle, err := store.ResolveAction("elite-dangerous/set-throttle")
	if err != nil || !setThrottle.SequenceEligible {
		t.Fatalf("set-throttle sequence eligibility = %+v, err = %v", setThrottle, err)
	}
	distanceRegions, err := store.ResolveAction("elite-dangerous/request-docking-distance-regions")
	if err != nil {
		t.Fatal(err)
	}
	if distanceRegions.Runtime != PpOcrTextRegionsActionRuntimeV1 || distanceRegions.RuntimeProfile != "ocr/text-regions" ||
		distanceRegions.Execution.Completion != CompletionReturn || len(distanceRegions.RegistrableAs) != 0 {
		t.Fatalf("request-docking-distance-regions action = %+v", distanceRegions)
	}
	leftPanelTabState, err := store.ResolveAction("elite-dangerous/left-panel-tab-state")
	if err != nil {
		t.Fatal(err)
	}
	if leftPanelTabState.Runtime != ObservationRuntimeV1 ||
		!reflect.DeepEqual(leftPanelTabState.RegistrableAs, []string{RegistrationMonitor, RegistrationReaction}) {
		t.Fatalf("left-panel-tab-state action = %+v", leftPanelTabState)
	}
	if _, err := store.ResolveAction("elite-dangerous/contacts-tab-state"); err == nil {
		t.Fatal("retired contacts-tab-state action still resolves")
	}
	if _, err := store.ResolveAction("elite-dangerous/request-docking-distance-text"); err == nil {
		t.Fatal("retired fixed request-docking-distance-text action still resolves")
	}
}

func TestStoreRejectsInvalidActionAndRegistrationContracts(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"old schema", `{"schemaVersion":4,"description":"Valid.","runtimeProfiles":{},"actions":{},"registrations":{}}`, "schemaVersion"},
		{"missing actions", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "actions is required"},
		{"missing registrations", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{},"ephemeralActionSequence":{"allowedActions":[]}}`, "registrations is required"},
		{"missing sequence declaration", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{},"registrations":{}}`, "ephemeralActionSequence.allowedActions is required"},
		{"missing runtime profiles", `{"schemaVersion":6,"description":"Valid.","actions":{},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "runtimeProfiles is required"},
		{"unknown field", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{},"extra":true}`, "unknown field"},
		{"sequence unknown action", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{},"ephemeralActionSequence":{"allowedActions":["a"]},"registrations":{}}`, "unknown action"},
		{"sequence duplicate", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"windows-key-action-v1","execution":{"completion":"return"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":["a","a"]},"registrations":{}}`, "duplicate action"},
		{"sequence unsupported runtime", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"private-runtime-v1","execution":{"completion":"return"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":["a"]},"registrations":{}}`, "unsupported runtime"},
		{"sequence loop child", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"windows-streaming-action-v1","execution":{"completion":"stream","lifecycle":"loop","interruptible":true},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":["a"]},"registrations":{}}`, "must be linear and interruptible"},
		{"action path", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Modules/a","runtime":"r","execution":{"completion":"return"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "below Actions/"},
		{"missing registrable", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return"}}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "registrableAs"},
		{"missing execution", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "execution"},
		{"return lifecycle", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return","lifecycle":"linear"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "forbids lifecycle"},
		{"stream lifecycle", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"stream","interruptible":true},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "lifecycle"},
		{"stream interruptible", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"stream","lifecycle":"linear"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "interruptible"},
		{"unknown registration type", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return"},"registrableAs":["timer"]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{}}`, "unsupported registration type"},
		{"unknown action", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{"m":{"type":"monitor","action":"missing","input":{},"monitor":{"intervalMs":1,"emit":{"stream":"s","eventType":"e"}}}}}`, "unknown action"},
		{"not declared", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return"},"registrableAs":[]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{"m":{"type":"monitor","action":"a","input":{},"monitor":{"intervalMs":1,"emit":{"stream":"s","eventType":"e"}}}}}`, "not declared"},
		{"zero interval", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return"},"registrableAs":["monitor"]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{"m":{"type":"monitor","action":"a","input":{},"monitor":{"intervalMs":0,"emit":{"stream":"s","eventType":"e"}}}}}`, "intervalMs"},
		{"invalid regex", `{"schemaVersion":6,"description":"Valid.","runtimeProfiles":{},"actions":{"a":{"path":"Actions/a","runtime":"r","execution":{"completion":"return"},"registrableAs":["reaction"]}},"ephemeralActionSequence":{"allowedActions":[]},"registrations":{"r":{"type":"reaction","action":"a","input":{},"reaction":{"stream":"s","eventType":"e","match":{"payload.x":"["}}}}}`, "regex is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			ruleRoot := filepath.Join(root, "Game.exe")
			if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ruleRoot, RuleFilename), []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ruleRoot, AgentsFilename), []byte("# Game\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Resolve("Game.exe"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestResolveActionRejectsDuplicateOrMissing(t *testing.T) {
	root := testRulesRoot(t)
	writeRule(t, root, "Other.exe", "Other.", "# Other\n", map[string]ActionDeclaration{
		"crimson-desert/inventory": {Path: "Actions/inventory", Runtime: ObservationRuntimeV1, Execution: returnExecution(), RegistrableAs: []string{}},
	}, map[string]RegistrationDeclaration{})
	for _, rule := range []string{"CrimsonDesert.exe", "Other.exe"} {
		if err := os.MkdirAll(filepath.Join(root, rule, "Actions", "inventory"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveAction("crimson-desert/inventory"); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := store.ResolveAction("missing/action"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing error = %v", err)
	}
}

func testRulesRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRule(t, root, "CrimsonDesert.exe", "Read the live Rule before acting.", "# Guidance\n", map[string]ActionDeclaration{
		"crimson-desert/inventory": {Path: "Actions/inventory", Runtime: ObservationRuntimeV1, Execution: returnExecution(), RegistrableAs: []string{RegistrationMonitor, RegistrationReaction}},
	}, map[string]RegistrationDeclaration{})
	return root
}

func writeRule(t *testing.T, root, id, description, agents string, actions map[string]ActionDeclaration, registrations map[string]RegistrationDeclaration) {
	t.Helper()
	if actions == nil {
		actions = map[string]ActionDeclaration{}
	}
	if registrations == nil {
		registrations = map[string]RegistrationDeclaration{}
	}
	ruleRoot := filepath.Join(root, id)
	if err := os.MkdirAll(ruleRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := Descriptor{
		SchemaVersion: 6, Description: description,
		RuntimeProfiles: map[string]RuntimeProfileDeclaration{},
		Actions:         actions, EphemeralActionSequence: &EphemeralActionSequenceDeclaration{AllowedActions: []string{}}, Registrations: registrations,
	}
	encoded, err := json.MarshalIndent(descriptor, "", "  ")
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

func returnExecution() *ActionExecutionDeclaration {
	return &ActionExecutionDeclaration{Completion: CompletionReturn}
}
