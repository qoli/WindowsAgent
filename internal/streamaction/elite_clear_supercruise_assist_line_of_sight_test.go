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
	spheres      []map[string]any
	sphereIndex  int
	controls     []string
	vectorOps    []string
	throttles    []int
}

func (c *clearSupercruiseAssistLineOfSightCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
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
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/supercruise-sphere-direction":
		if c.sphereIndex >= len(c.spheres) {
			return nil, errors.New("unexpected sphere observation")
		}
		result := c.spheres[c.sphereIndex]
		c.sphereIndex++
		return json.Marshal(result)
	case "elite-dangerous/ship-attitude-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.Marshal(map[string]any{"selection": control, "holdMs": inputs["holdMs"]})
	case "elite-dangerous/ship-attitude-vector-hold":
		operation, _ := inputs["operation"].(string)
		control, _ := inputs["control"].(string)
		c.vectorOps = append(c.vectorOps, operation+":"+control)
		if operation == "START" {
			c.controls = append(c.controls, control)
			return json.Marshal(map[string]any{"operation": "STARTED", "control": control, "leaseId": "fixture-lease"})
		}
		return json.Marshal(map[string]any{"operation": "STOPPED", "control": control, "leaseId": inputs["leaseId"]})
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

func detectedSphereWithControl(x, y, radius, clearance float64, control string) map[string]any {
	return map[string]any{
		"sphere": map[string]any{
			"state": "DETECTED", "centerX": x, "centerY": y, "radiusPixels": radius,
			"signedLimbClearancePixels": clearance, "confidencePermille": int64(850),
		},
		"direction": map[string]any{"state": "READY", "control": control, "reason": "fixture"},
	}
}

func detectedSphere(x, y, radius, clearance float64) map[string]any {
	return detectedSphereWithControl(x, y, radius, clearance, "YAW_RIGHT")
}

func absentSphere() map[string]any {
	return map[string]any{
		"sphere": map[string]any{
			"state": "ABSENT", "centerX": nil, "centerY": nil, "radiusPixels": nil,
			"signedLimbClearancePixels": nil, "confidencePermille": int64(0),
		},
		"direction": map[string]any{"state": "UNKNOWN", "control": nil, "reason": "fixture absent"},
	}
}

func successFlightStates() []string {
	states := []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"}
	for range 60 {
		states = append(states, "SUPERCRUISE")
	}
	return append(states, "SUPERCRUISE", "SUPERCRUISE")
}

func TestEliteClearSupercruiseAssistLineOfSightConfirmsDirectionThenExecutesFixedTurnAndSeparation(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: successFlightStates(),
		spheres: []map[string]any{
			detectedSphereWithControl(1200, 540, 420, -180, "PITCH_UP_YAW_LEFT"),
			detectedSphereWithControl(1340, 701, 94, 320, "PITCH_UP_YAW_LEFT"),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, reporter)
	if err != nil || !contains(string(output), `"directionConfirmations":2`) || !contains(string(output), `"fixedTurnDurationMs":6400`) || !contains(string(output), `"fixedOutwardTurnCompleted":true`) || !contains(string(output), `"separationDurationMs":30000`) || !contains(string(output), `"separationSamples":60`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 8 {
		t.Fatalf("controls=%v", caller.controls)
	}
	if len(caller.vectorOps) != 16 {
		t.Fatalf("vector operations=%v", caller.vectorOps)
	}
	for i := 0; i < len(caller.vectorOps); i += 2 {
		if caller.vectorOps[i] != "START:PITCH_UP_YAW_LEFT" || caller.vectorOps[i+1] != "STOP:PITCH_UP_YAW_LEFT" {
			t.Fatalf("vector operations=%v", caller.vectorOps)
		}
	}
	if caller.sphereIndex != 2 {
		t.Fatalf("sphere observations=%d want=2", caller.sphereIndex)
	}
	wantThrottles := []int{0, 100, 0}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	for i := range wantThrottles {
		if caller.throttles[i] != wantThrottles[i] {
			t.Fatalf("throttles=%v", caller.throttles)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"CONFIRMING_OUTWARD_DIRECTION", "EXECUTING_FIXED_OUTWARD_TURN", "SEPARATION_FLIGHT", "VERIFYING_PROMPT_CLEAR", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing %s events=%s", phase, joined)
		}
	}
}

func TestEliteClearSupercruiseAssistLineOfSightFailsStoppedWhenSphereDirectionUnknown(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"},
		spheres:      []map[string]any{absentSphere()},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_SPHERE_DIRECTION_UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || len(caller.throttles) != 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("controls=%v throttles=%v", caller.controls, caller.throttles)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightDoesNotTreatUnknownPromptAsClear(t *testing.T) {
	states := []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"}
	for range 60 {
		states = append(states, "SUPERCRUISE")
	}
	for range 8 {
		states = append(states, "UNKNOWN")
	}
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: states,
		spheres:      []map[string]any{detectedSphere(1200, 540, 420, -180), detectedSphere(1340, 701, 94, 320)},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_PROMPT_NOT_CLEAR_AFTER_SEPARATION") {
		t.Fatalf("error=%v", err)
	}
	if caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}

func TestEliteClearSupercruiseAssistLineOfSightRejectsDirectionDisagreementBeforeInput(t *testing.T) {
	caller := &clearSupercruiseAssistLineOfSightCaller{
		flightStates: []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"},
		spheres: []map[string]any{
			detectedSphereWithControl(1200, 540, 420, -180, "YAW_RIGHT"),
			detectedSphereWithControl(1340, 701, 94, 320, "PITCH_UP"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteClearSupercruiseAssistLineOfSightPackage(t), map[string]any{"targetName": "OBAMA REACH"}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "LINE_OF_SIGHT_OUTWARD_DIRECTION_NOT_STABLE") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || len(caller.throttles) != 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("controls=%v throttles=%v", caller.controls, caller.throttles)
	}
}
