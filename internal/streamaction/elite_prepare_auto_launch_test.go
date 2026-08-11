package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type prepareAutoLaunchCaller struct {
	controls    []string
	focusStates []string
	focusIndex  int
}

func (c *prepareAutoLaunchCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	if id == "elite-dangerous/station-service-focus" {
		if c.focusIndex >= len(c.focusStates) {
			return nil, errors.New("missing station service focus fixture")
		}
		state := c.focusStates[c.focusIndex]
		c.focusIndex++
		indexes := map[string]any{"REFUEL": 0, "REPAIR": 1, "RESTOCK": 2, "LAYER_SWITCH": 3, "UNKNOWN": nil}
		return json.Marshal(map[string]any{
			"schemaVersion": 1,
			"focus":         map[string]any{"state": state, "index": indexes[state], "reason": state + "_FIXTURE"},
		})
	}
	if id != "elite-dangerous/ui-control" {
		return nil, errors.New("unexpected prepare-auto-launch child Action: " + id)
	}
	control, ok := inputs["control"].(string)
	if !ok {
		return nil, errors.New("UI control is not a string")
	}
	c.controls = append(c.controls, control)
	logical := "UI_" + control[0:1] + control[1:]
	if control == "SELECT" {
		logical = "UI_Select"
	}
	return json.Marshal(map[string]any{
		"schemaVersion": 1, "selection": control, "control": logical,
		"key": "Key_Space", "activePreset": "ControlPadKeyboard",
		"bindingFile": "ControlPadKeyboard.4.2.binds", "bindingSource": "frontier-active-preset-v1",
		"backend": "sendinput-scancode", "scanCode": 57, "extended": false, "holdMs": 40,
	})
}

func loadElitePrepareAutoLaunchPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "prepare-auto-launch"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestElitePrepareAutoLaunchSafeModeCorrectsRememberedRestockFocus(t *testing.T) {
	caller := &prepareAutoLaunchCaller{focusStates: []string{"RESTOCK", "RESTOCK", "REFUEL", "REPAIR"}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadElitePrepareAutoLaunchPackage(t), map[string]any{"activateAutoLaunch": false}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DOWN", "DOWN", "DOWN", "DOWN",
		"UP", "UP", "UP", "RIGHT", "RIGHT", "SELECT", "RIGHT", "SELECT",
		"DOWN", "DOWN", "DOWN", "DOWN", "UP",
	}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls=%v", caller.controls)
	}
	if !contains(string(output), `"controlCount":17`) ||
		!contains(string(output), `"initialServiceFocus":"RESTOCK"`) ||
		!contains(string(output), `"rightMovesToRefuel":2`) ||
		!contains(string(output), `"refuelAttempted":true`) ||
		!contains(string(output), `"repairAttempted":true`) ||
		!contains(string(output), `"autoLaunchSelected":false`) ||
		!contains(string(output), `"awaitingFinalSelect":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestElitePrepareAutoLaunchActivationSendsFinalSelect(t *testing.T) {
	caller := &prepareAutoLaunchCaller{focusStates: []string{"REFUEL", "REFUEL", "REPAIR"}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadElitePrepareAutoLaunchPackage(t), map[string]any{"activateAutoLaunch": true}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DOWN", "DOWN", "DOWN", "DOWN",
		"UP", "UP", "UP", "SELECT", "RIGHT", "SELECT",
		"DOWN", "DOWN", "DOWN", "DOWN", "UP", "SELECT",
	}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls=%v", caller.controls)
	}
	if !contains(string(output), `"controlCount":16`) ||
		!contains(string(output), `"autoLaunchSelected":true`) ||
		!contains(string(output), `"awaitingFinalSelect":false`) {
		t.Fatalf("output=%s", output)
	}
}

func TestElitePrepareAutoLaunchFailsClosedOnUnknownFocus(t *testing.T) {
	caller := &prepareAutoLaunchCaller{focusStates: []string{"UNKNOWN"}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadElitePrepareAutoLaunchPackage(t), map[string]any{"activateAutoLaunch": false}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "station service focus is UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	for _, control := range caller.controls {
		if control == "SELECT" {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
}
