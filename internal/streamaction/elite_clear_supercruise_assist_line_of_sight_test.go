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
	coreOutput   map[string]any
	coreErr      error
	coreCalls    int
	childCalls   []string
	throttles    []int
}

func (c *clearSupercruiseAssistLineOfSightCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	c.childCalls = append(c.childCalls, id)
	switch id {
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}, "source": map[string]any{"text": "fixture"}})
	case "elite-dangerous/fixed-supercruise-sphere-separation":
		c.coreCalls++
		if c.coreErr != nil {
			return nil, c.coreErr
		}
		return json.Marshal(c.coreOutput)
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.Marshal(map[string]any{"control": "SetSpeedZero"})
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

func successfulCenteredSphereCore() map[string]any {
	return map[string]any{
		"completed":           true,
		"fixedTurnDurationMs": int64(6400), "separationDurationMs": int64(30000),
		"finalSupercruiseConfirmed": true, "finalCommandedThrottle": int64(0),
		"control": "PITCH_DOWN_YAW_RIGHT", "directionConfirmations": int64(2),
		"turnPulses": int64(8), "separationSamples": int64(60), "sampleCount": int64(73),
	}
}

func TestEliteClearSupercruiseAssistLineOfSightDelegatesFixedTurnAndSeparationToSharedCore(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
			"SUPERCRUISE",
			"SUPERCRUISE",
		},
		coreOutput: successfulCenteredSphereCore(),
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, reporter)
	if err != nil || !contains(string(output), `"directionConfirmations":2`) || !contains(string(output), `"fixedTurnDurationMs":6400`) || !contains(string(output), `"fixedOutwardTurnCompleted":true`) || !contains(string(output), `"separationDurationMs":30000`) || !contains(string(output), `"separationSamples":60`) || !contains(string(output), `"finalSupercruiseConfirmed":true`) || !contains(string(output), `"finalCommandedThrottle":0`) || !contains(string(output), `"sampleCount":77`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.coreCalls != 1 || len(caller.throttles) != 0 {
		t.Fatalf("coreCalls=%d throttles=%v", caller.coreCalls, caller.throttles)
	}
	wantChildren := []string{
		"elite-dangerous/flight-status",
		"elite-dangerous/flight-status",
		"elite-dangerous/fixed-supercruise-sphere-separation",
		"elite-dangerous/flight-status",
		"elite-dangerous/flight-status",
	}
	if len(caller.childCalls) != len(wantChildren) {
		t.Fatalf("childCalls=%v", caller.childCalls)
	}
	for index := range wantChildren {
		if caller.childCalls[index] != wantChildren[index] {
			t.Fatalf("childCalls=%v", caller.childCalls)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"CONFIRMING_GATE", "CLEARING_CENTERED_SPHERE", "VERIFYING_PROMPT_CLEAR", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing %s events=%s", phase, joined)
		}
	}
}

func TestEliteClearSupercruiseAssistLineOfSightFailsStoppedWhenSharedDirectionCoreFails(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"},
		coreErr:      errors.New("CENTERED_SUPERCRUISE_SPHERE_DIRECTION_UNKNOWN"),
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "CENTERED_SUPERCRUISE_SPHERE_DIRECTION_UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if caller.coreCalls != 1 || len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("coreCalls=%d throttles=%v", caller.coreCalls, caller.throttles)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightDoesNotTreatUnknownPromptAsClear(t *testing.T) {
	states := []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"}
	for range 8 {
		states = append(states, "UNKNOWN")
	}
	caller := &clearSupercruiseAssistLineOfSightCaller{flightStates: states, coreOutput: successfulCenteredSphereCore()}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_PROMPT_NOT_CLEAR_AFTER_SEPARATION") {
		t.Fatalf("error=%v", err)
	}
	if caller.coreCalls != 1 || len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("coreCalls=%d throttles=%v", caller.coreCalls, caller.throttles)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightRejectsIncompleteSharedCorePostcondition(t *testing.T) {
	core := successfulCenteredSphereCore()
	core["finalSupercruiseConfirmed"] = false
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"},
		coreOutput:   core,
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "incomplete fixed-clearance postcondition") {
		t.Fatalf("error=%v", err)
	}
	if caller.coreCalls != 1 || len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("coreCalls=%d throttles=%v", caller.coreCalls, caller.throttles)
	}
}
