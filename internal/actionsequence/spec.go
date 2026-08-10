// Package actionsequence defines the bounded, ephemeral Action Sequence contract.
package actionsequence

import (
	"errors"
	"fmt"
	"strings"
)

const (
	RuntimeID = "windows-ephemeral-action-sequence-v1"
	ActionID  = "system/ephemeral-action-sequence"
	MaxSteps  = 20
)

// Request is one immutable sequence submitted for immediate execution.
type Request struct {
	RuleID string `json:"ruleId"`
	Steps  []Step `json:"steps"`
}

// Step invokes exactly one existing Action with literal JSON inputs.
type Step struct {
	Action string         `json:"action"`
	Inputs map[string]any `json:"inputs"`
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.RuleID) == "" || strings.TrimSpace(r.RuleID) != r.RuleID {
		return errors.New("Action Sequence ruleId must be a canonical non-empty string")
	}
	if len(r.Steps) == 0 || len(r.Steps) > MaxSteps {
		return fmt.Errorf("Action Sequence must contain from 1 through %d steps", MaxSteps)
	}
	for index, step := range r.Steps {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("Action Sequence step %d: %w", index+1, err)
		}
	}
	return nil
}

func (s Step) Validate() error {
	if strings.TrimSpace(s.Action) == "" || strings.TrimSpace(s.Action) != s.Action {
		return errors.New("action must be a canonical non-empty string")
	}
	if s.Action == ActionID {
		return errors.New("an Action Sequence cannot invoke itself")
	}
	if s.Inputs == nil {
		return errors.New("inputs must be an object")
	}
	return nil
}
