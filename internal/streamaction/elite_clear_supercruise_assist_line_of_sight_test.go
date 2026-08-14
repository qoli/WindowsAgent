package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type clearSupercruiseAssistLineOfSightCaller struct {
	flightStates []string
	flightIndex  int
	direction    map[string]any
	targets      []map[string]any
	targetIndex  int
	controls     []string
	throttles    []int
}

func (c *clearSupercruiseAssistLineOfSightCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"text":"fixture"}`), nil
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}, "source": map[string]any{"text": "fixture"}})
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 75: "SetSpeed75"}[percent]})
	case "elite-dangerous/supercruise-line-of-sight-direction":
		return json.Marshal(map[string]any{"direction": c.direction})
	case "elite-dangerous/ship-attitude-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.Marshal(map[string]any{"selection": control, "holdMs": inputs["holdMs"]})
	case "elite-dangerous/supercruise-target-position":
		if c.targetIndex >= len(c.targets) {
			return nil, errors.New("unexpected target-position observation")
		}
		target := c.targets[c.targetIndex]
		c.targetIndex++
		return json.Marshal(map[string]any{"target": target})
	default:
		return nil, errors.New("unexpected clear-line-of-sight child Action: " + id)
	}
}

func loadEliteClearSupercruiseAssistLineOfSightPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "clear-supercruise-assist-line-of-sight"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func readyLineOfSightDirection() map[string]any {
	return map[string]any{
		"state": "READY", "control": "YAW_RIGHT", "reason": "fixture",
		"initialProjectionPixels": 160.0,
	}
}

func bypassTarget(offsetX float64) map[string]any {
	return map[string]any{
		"state": "DETECTED", "presentation": "DASHED", "offsetX": offsetX,
		"offsetY": 0.0, "reason": "fixture",
	}
}

func TestEliteClearSupercruiseAssistLineOfSightTurnsPastTargetThenWaitsForPromptClear(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"UNKNOWN", "UNKNOWN",
		},
		direction: readyLineOfSightDirection(),
		targets:   []map[string]any{bypassTarget(60), bypassTarget(-20), bypassTarget(-110)},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteClearSupercruiseAssistLineOfSightPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) ||
		!contains(string(output), `"control":"YAW_RIGHT"`) ||
		!contains(string(output), `"turnPulses":3`) ||
		!contains(string(output), `"bypassFlightSamples":2`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 3 || caller.controls[0] != "YAW_RIGHT" || caller.controls[2] != "YAW_RIGHT" {
		t.Fatalf("controls=%v", caller.controls)
	}
	wantThrottles := []int{0, 75, 0}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"TURNING_TO_BYPASS"`) ||
		!contains(joined, `"phase":"BYPASS_FLIGHT"`) ||
		!contains(joined, `"phase":"COMPLETED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightFailsStoppedWhenDirectionUnknown(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"},
		direction: map[string]any{
			"state": "UNKNOWN", "control": nil, "reason": "FOCUS_FRAME_TOO_CLOSE_TO_SCREEN_CENTER_FOR_BYPASS_DIRECTION",
			"initialProjectionPixels": nil,
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteClearSupercruiseAssistLineOfSightPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		&fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_DIRECTION_UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || len(caller.throttles) != 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("controls=%v throttles=%v", caller.controls, caller.throttles)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightFailsOnUnobservedTurnProgress(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
		},
		direction: readyLineOfSightDirection(),
		targets:   []map[string]any{bypassTarget(160), bypassTarget(160), bypassTarget(160), bypassTarget(160)},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteClearSupercruiseAssistLineOfSightPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		&fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_TURN_NO_PROGRESS") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("failure compensation throttles=%v", caller.throttles)
	}
}
