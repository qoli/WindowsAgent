package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type closeLeftPanelCaller struct {
	states   []string
	controls []string
}

func (c *closeLeftPanelCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/left-panel-tab-state":
		if len(c.states) == 0 {
			return nil, errors.New("unexpected left-panel observation")
		}
		state := c.states[0]
		c.states = c.states[1:]
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected close-left-panel child Action: " + id)
	}
}

func loadEliteCloseLeftPanelPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "close-left-panel"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteCloseLeftPanelConfirmsTwoAbsentSamplesAcrossUnknownNoise(t *testing.T) {
	caller := &closeLeftPanelCaller{states: []string{"CONTACTS", "ABSENT", "UNKNOWN", "ABSENT"}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseLeftPanelPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.controls, []string{"FOCUS_LEFT_PANEL"}) {
		t.Fatalf("controls=%v", caller.controls)
	}
	if !contains(string(output), `"closed":true`) || !contains(string(output), `"commandSent":true`) || !contains(string(output), `"finalState":"ABSENT"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteCloseLeftPanelNoOpsOnlyOnKnownAbsentState(t *testing.T) {
	caller := &closeLeftPanelCaller{states: []string{"ABSENT"}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseLeftPanelPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 || !contains(string(output), `"commandSent":false`) {
		t.Fatalf("controls=%v output=%s", caller.controls, output)
	}
}

func TestEliteCloseLeftPanelRejectsUnknownInitialStateWithoutInput(t *testing.T) {
	caller := &closeLeftPanelCaller{states: []string{"UNKNOWN"}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseLeftPanelPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "left panel state is UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("controls=%v", caller.controls)
	}
}
