package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type selectAndLockDestinationCaller struct {
	contacts []string
	regions  []json.RawMessage
	controls []string
}

func (c *selectAndLockDestinationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/contacts-tab-state":
		if len(c.contacts) == 0 {
			return nil, errors.New("unexpected Contacts observation")
		}
		state := c.contacts[0]
		c.contacts = c.contacts[1:]
		return json.Marshal(map[string]any{"contactsTab": map[string]any{"state": state}})
	case "elite-dangerous/navigation-list-text-regions":
		if len(c.regions) == 0 {
			return nil, errors.New("unexpected Navigation OCR observation")
		}
		value := c.regions[0]
		c.regions = c.regions[1:]
		return value, nil
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected select-and-lock-destination child Action: " + id)
	}
}

func navigationTargetRow(text string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"regions": []any{map[string]any{
			"detectionConfidence":   0.92,
			"recognitionConfidence": 0.98,
			"text":                  text,
			"referencePoints": []any{
				map[string]any{"x": 520, "y": 480}, map[string]any{"x": 700, "y": 480},
				map[string]any{"x": 700, "y": 510}, map[string]any{"x": 520, "y": 510},
			},
			"leftContext": map[string]any{"w": 1, "h": 1, "pixels": []any{0}},
		}},
	})
	return value
}

func loadEliteSelectAndLockDestinationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "select-and-lock-destination"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteSelectAndLockDestinationOwnsPanelAndAcceptsExistingNamedLock(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"ABSENT", "ABSENT", "SELECTED", "SELECTED", "ABSENT", "ABSENT"},
		regions:  []json.RawMessage{navigationTargetRow("< NAV BEACON >"), navigationTargetRow("< NAV BEACON >")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "PREVIOUS_PANEL", "PREVIOUS_PANEL", "FOCUS_LEFT_PANEL"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("controls=%v want=%v", caller.controls, wantControls)
	}
	if !contains(string(output), `"result":"EXISTING"`) || !contains(string(output), `"restoredView":true`) || !contains(string(output), `"targetLocked":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectAndLockDestinationFailsWithoutNamedVisibleTarget(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"SELECTED", "SELECTED"},
		regions: []json.RawMessage{
			navigationTargetRow("LHS 6050"), navigationTargetRow("LHS 6050"),
			navigationTargetRow("LHS 6050"), navigationTargetRow("LHS 6050"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "named Navigation target did not produce two consecutive known observations") {
		t.Fatalf("error=%v", err)
	}
	for _, control := range caller.controls {
		if control == "SELECT" {
			t.Fatalf("missing target triggered SELECT: controls=%v", caller.controls)
		}
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
