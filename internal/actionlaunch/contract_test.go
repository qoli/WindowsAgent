package actionlaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qoli/WindowsAgent/internal/actionsequence"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

func TestContractLoadsAndValidatesStreamingInputWithoutExecuting(t *testing.T) {
	rulesRoot := t.TempDir()
	ruleRoot := filepath.Join(rulesRoot, "game.exe")
	workflowRoot := filepath.Join(ruleRoot, "Actions", "workflow")
	if err := os.MkdirAll(workflowRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(ruleRoot, rules.RuleFilename), `{
  "schemaVersion":6,
  "description":"Streaming fixture.",
  "runtimeProfiles":{},
  "actions":{
    "game/workflow":{"path":"Actions/workflow","runtime":"windows-streaming-action-v1","execution":{"completion":"stream","lifecycle":"linear","interruptible":true},"registrableAs":[]}
  },
  "ephemeralActionSequence":{"allowedActions":["game/workflow"]},
  "registrations":{}
}`)
	writeTestFile(t, filepath.Join(ruleRoot, rules.AgentsFilename), "# Fixture\n")
	writeStreamingPackage(t, workflowRoot)
	ruleStore, err := rules.New(rulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := New(
		ruleStore, fakeObservationExecutor{}, fakeRegionCapturer{}, &fakeOCRRecognizer{}, fakeInputExecutor{},
		func() (foreground.Info, error) { return foreground.Info{ExecutableName: "game.exe"}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := executor.Contract("game/workflow")
	if err != nil {
		t.Fatal(err)
	}
	if contract.Action.ID != "game/workflow" || contract.Title != "Fixture" || len(contract.InputSchema) == 0 {
		t.Fatalf("contract = %+v", contract)
	}
	if err := contract.ValidateInput(map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if err := contract.ValidateInput(map[string]any{}); err == nil {
		t.Fatal("invalid input was accepted")
	}
}

func TestRepositoryEphemeralSequenceContractsBuildOneStrictToolSchema(t *testing.T) {
	rulesRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules"))
	if err != nil {
		t.Fatal(err)
	}
	ruleStore, err := rules.New(rulesRoot)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := New(
		ruleStore, fakeObservationExecutor{}, fakeRegionCapturer{}, &fakeOCRRecognizer{}, fakeInputExecutor{},
		func() (foreground.Info, error) { return foreground.Info{ExecutableName: "EliteDangerous64.exe"}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	actions, _, err := ruleStore.ReadActions("EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var candidates []actionsequence.Candidate
	for _, action := range actions {
		if !action.SequenceEligible {
			continue
		}
		contract, err := executor.Contract(action.ID)
		if err != nil {
			t.Fatalf("contract %s: %v", action.ID, err)
		}
		candidates = append(candidates, actionsequence.Candidate{
			ID: action.ID, Description: contract.Title, InputSchema: contract.InputSchema,
		})
	}
	if len(candidates) != 14 {
		t.Fatalf("sequence candidate count = %d", len(candidates))
	}
	schema, err := actionsequence.BuildToolSchema("EliteDangerous64.exe", candidates)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil || !json.Valid(encoded) || schema.Name != "run_action_sequence" || !schema.Strict {
		t.Fatalf("schema = %+v, err = %v", schema, err)
	}
}
