package actionsequence

import (
	"strings"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	valid := Request{RuleID: "Game.exe", Steps: []Step{{Action: "game/read", Inputs: map[string]any{}}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]Request{
		"missing rule":    {Steps: valid.Steps},
		"missing steps":   {RuleID: "Game.exe"},
		"too many steps":  {RuleID: "Game.exe", Steps: make([]Step, MaxSteps+1)},
		"missing inputs":  {RuleID: "Game.exe", Steps: []Step{{Action: "game/read"}}},
		"nested sequence": {RuleID: "Game.exe", Steps: []Step{{Action: ActionID, Inputs: map[string]any{}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.Validate(); err == nil {
				t.Fatal("invalid Action Sequence was accepted")
			}
		})
	}
}

func TestStepNumberIsReported(t *testing.T) {
	err := (Request{RuleID: "Game.exe", Steps: []Step{{Action: "game/read", Inputs: map[string]any{}}, {}}}).Validate()
	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("error = %v", err)
	}
}
