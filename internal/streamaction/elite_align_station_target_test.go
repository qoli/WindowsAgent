package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
)

type alignStationTargetCaller struct {
	observations    []json.RawMessage
	index           int
	throttles       []int
	controls        []string
	holds           []int
	holdControls    []string
	holdOps         []string
	compassFailures []error
}

func (c *alignStationTargetCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.RawMessage(`{"schemaVersion":1,"selection":"0","control":"SetSpeedZero"}`), nil
	case "elite-dangerous/compass":
		if len(c.compassFailures) > 0 {
			err := c.compassFailures[0]
			c.compassFailures = c.compassFailures[1:]
			return nil, err
		}
		if c.index >= len(c.observations) {
			return nil, errors.New("unexpected Compass observation")
		}
		result := c.observations[c.index]
		c.index++
		return result, nil
	case "elite-dangerous/ship-attitude-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("attitude control is not a string")
		}
		c.controls = append(c.controls, control)
		hold, ok := inputs["holdMs"].(int64)
		if !ok {
			return nil, errors.New("attitude holdMs is not an integer")
		}
		c.holds = append(c.holds, int(hold))
		return json.Marshal(map[string]any{"schemaVersion": 1, "selection": control, "control": control, "holdMs": hold})
	case "elite-dangerous/ship-attitude-hold", "elite-dangerous/ship-attitude-vector-hold":
		operation, ok := inputs["operation"].(string)
		if !ok {
			return nil, errors.New("attitude hold operation is not a string")
		}
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("attitude hold control is not a string")
		}
		c.holdOps = append(c.holdOps, operation)
		c.holdControls = append(c.holdControls, control)
		state := "ACTIVE"
		reason := any(nil)
		if operation == "STOP" {
			state = "RELEASED"
			reason = "EXPLICIT"
		}
		return json.Marshal(map[string]any{
			"schemaVersion": 1, "operation": operation, "selection": control, "control": control,
			"leaseId": "key_00000000000000000000000000000001", "leaseMs": 2500,
			"leaseState": state, "releaseReason": reason,
		})
	default:
		return nil, errors.New("unexpected align-station-target child Action: " + id)
	}
}

func TestEliteAlignStationTargetRetriesTransientCompassNotVisible(t *testing.T) {
	caller := &alignStationTargetCaller{
		compassFailures: []error{
			&scriptlaunch.Error{Code: "COMPASS_NOT_VISIBLE", Stage: "executing-script", Cause: errors.New("HUD inertia")},
			&scriptlaunch.Error{Code: "COMPASS_NOT_VISIBLE", Stage: "executing-script", Cause: errors.New("HUD inertia")},
		},
		observations: []json.RawMessage{
			alignObservation("SOLID", 3, 0, 3, true),
			alignObservation("SOLID", 2, 0, 2, true),
			alignObservation("SOLID", 1, 0, 1, true),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) || !contains(string(output), `"sampleCount":5`) {
		t.Fatalf("output=%s", output)
	}
	retries := 0
	for _, payload := range reporter.payloads {
		if contains(string(payload), `"reason":"COMPASS_NOT_VISIBLE_RETRY"`) {
			retries++
		}
	}
	if retries != 2 {
		t.Fatalf("retry events=%d payloads=%v", retries, reporter.payloads)
	}
}

func alignObservation(presentation string, offsetX, offsetY int, distance float64, inside bool) json.RawMessage {
	hemisphere := "FRONT"
	if presentation == "HOLLOW" {
		hemisphere = "REAR"
	}
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 3,
		"target": map[string]any{
			"detected": true, "presentation": presentation, "hemisphere": hemisphere,
			"offsetX": offsetX, "offsetY": offsetY, "centerDistancePixels": distance,
			"centerZone": map[string]any{"inside": inside},
		},
	})
	return value
}

func loadEliteAlignStationTargetPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "align-station-target"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteAlignStationTargetTurnsRearMarkerThenStablyCenters(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", -10, 0, 10, false),
		alignObservation("SOLID", 8, 0, 8, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"centerContactCount":3,"commandCount":2,"completed":true,"finalObservation":{"schemaVersion":3,"target":{"centerDistancePixels":1,"centerZone":{"inside":true},"detected":true,"hemisphere":"FRONT","offsetX":1,"offsetY":0,"presentation":"SOLID"}},"finalPhase":"COMPLETED","maxConsecutiveCenter":3,"mode":"ALIGN","sampleCount":6,"schemaVersion":1,"stableConfirmations":3,"task":"ALIGN_STATION_TARGET"}` {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	wantControls := []string{"YAW_RIGHT"}
	wantHolds := []int{120}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v", caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] || caller.holds[index] != wantHolds[index] {
			t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
		}
	}
	if strings.Join(caller.holdOps, ",") != "START,RENEW,STOP" || strings.Join(caller.holdControls, ",") != "YAW_LEFT,YAW_LEFT,YAW_LEFT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
}

func TestEliteAlignStationTargetBrakesResidualTurnDuringPresentationTransition(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 1, -8, 8.06, false),
		alignObservation("UNKNOWN", -12, -7, 13.89, false),
		alignObservation("UNKNOWN", -13, -7, 14.76, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"COMPLETED"`) {
		t.Fatalf("output=%s", output)
	}
	if strings.Join(caller.holdOps, ",") != "START,STOP" || strings.Join(caller.holdControls, ",") != "YAW_LEFT,YAW_LEFT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
	if len(caller.controls) != 1 || caller.controls[0] != "YAW_RIGHT" || caller.holds[0] != 120 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
	brakeEventFound := false
	for _, payload := range reporter.payloads {
		if contains(string(payload), `"reason":"AMBIGUOUS_TRANSITION_BRAKE"`) {
			brakeEventFound = true
			break
		}
	}
	if !brakeEventFound {
		t.Fatalf("payloads=%v", reporter.payloads)
	}
}

func TestEliteAlignStationTargetCanPreserveOwningWorkflowThrottle(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"stopBeforeAlign": false}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttles=%v", caller.throttles)
	}
}

func TestEliteAlignStationTargetUsesDiagonalPulseWhenBothFineComponentsMatter(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 5, 5, 7.071, false),
		alignObservation("SOLID", 3, 3, 4.0, true),
		alignObservation("SOLID", 2, 2, 2.828, true),
		alignObservation("SOLID", 1, 1, 1.414, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"stopBeforeAlign": false}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.holdOps, ",") != "START,STOP" || strings.Join(caller.holdControls, ",") != "PITCH_DOWN_YAW_RIGHT,PITCH_DOWN_YAW_RIGHT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetTracksMovingTargetPastTransientCenter(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 4, 0, 4, true),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 8, 0, 8, false),
		alignObservation("SOLID", 7, 0, 7, false),
		alignObservation("SOLID", 4, 0, 4, true),
		alignObservation("SOLID", 5, 0, 5, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 6, 0, 6, false),
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"mode": "TRACK", "trackingSamples": float64(10)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"mode":"TRACK"`) || !contains(string(output), `"finalPhase":"TRACKING_WINDOW_COMPLETED"`) || !contains(string(output), `"sampleCount":10`) || !contains(string(output), `"centerContactCount":6`) || !contains(string(output), `"maxConsecutiveCenter":3`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.controls) != 3 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetTrackKeepsCorrectingCurrentOffsetWithoutInferringStall(t *testing.T) {
	observations := make([]json.RawMessage, 10)
	for index := range observations {
		observations[index] = alignObservation("SOLID", 0, -7, 7, false)
	}
	caller := &alignStationTargetCaller{observations: observations}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"mode": "TRACK", "trackingSamples": float64(10)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"TRACKING_WINDOW_COMPLETED"`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.controls) != 9 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetUsesDominantFrontAxis(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -20, 4, 20.396, false),
		alignObservation("SOLID", 2, 8, 8.246, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"YAW_LEFT", "PITCH_DOWN"}
	wantHolds := []int{300, 120}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] || caller.holds[index] != wantHolds[index] {
			t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
		}
	}
}

func TestEliteAlignStationTargetUsesSustainedControlOutsideFineBand(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 45, 0, 45, false),
		alignObservation("SOLID", 42, 0, 42, false),
		alignObservation("SOLID", 35, 0, 35, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.holdOps, ",") != "START,RENEW,STOP" || strings.Join(caller.holdControls, ",") != "YAW_RIGHT,YAW_RIGHT,YAW_RIGHT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
	if len(caller.controls) != 2 || caller.controls[0] != "YAW_RIGHT" || caller.holds[0] != 300 || caller.controls[1] != "YAW_LEFT" || caller.holds[1] != 100 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"controlMode":"SUSTAINED"`) || !contains(joined, `"leaseState":"ACTIVE"`) ||
		!contains(joined, `"reason":"SUSTAINED_CONTROL_RELEASED"`) || !contains(joined, `"sampleDurationMs":`) ||
		!contains(joined, `"sampleIntervalMs":`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteAlignStationTargetUsesDiagonalSustainedControlForTwoFarAxes(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 38, -20, 42.94, false),
		alignObservation("SOLID", 35, -15, 38.08, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.holdOps, ",") != "START,STOP" ||
		strings.Join(caller.holdControls, ",") != "PITCH_UP_YAW_RIGHT,PITCH_UP_YAW_RIGHT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
}

func TestEliteAlignStationTargetReleasesSustainedControlAcrossTransientAmbiguousPresentation(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", -10, 9, 14.21, false),
		alignObservation("UNKNOWN", 24, 6, 24.74, false),
		alignObservation("HOLLOW", 20, 6, 20.88, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caller.holdOps, ",") != "START,STOP,START,STOP" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"reason":"SUSTAINED_CONTROL_RELEASED_FOR_AMBIGUOUS_OBSERVATION"`) ||
		!contains(joined, `"reason":"TARGET_PRESENTATION_UNKNOWN"`) ||
		!contains(joined, `"leaseState":"RELEASED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteAlignStationTargetContinuesAcrossExtendedRearToFrontAmbiguity(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 12, 2, 12.166, false),
		alignObservation("UNKNOWN", 8, 2, 8.246, false),
		alignObservation("UNKNOWN", 6, 2, 6.325, false),
		alignObservation("UNKNOWN", 4, 1, 4.123, false),
		alignObservation("UNKNOWN", -4, 1, 4.123, false),
		alignObservation("UNKNOWN", -8, 2, 8.246, false),
		alignObservation("UNKNOWN", -12, 2, 12.166, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"controlProfile": "SUPERCRUISE_ASSIST"}, caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"AMBIGUOUS_REAR_TRANSITION_CONTINUE"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteAlignStationTargetBrakesSupercruiseSustainedRelease(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 20, 0, 20, false),
		alignObservation("SOLID", 12, 0, 12, false),
		alignObservation("SOLID", 10, 0, 10, false),
		alignObservation("SOLID", 9, 0, 9, false),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"controlProfile": "SUPERCRUISE_ASSIST"}, caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if strings.Join(caller.holdOps, ",") != "START,STOP" || strings.Join(caller.holdControls, ",") != "YAW_LEFT,YAW_LEFT" {
		t.Fatalf("holdOps=%v holdControls=%v", caller.holdOps, caller.holdControls)
	}
	if len(caller.controls) != 1 || caller.controls[0] != "YAW_RIGHT" || caller.holds[0] != 300 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
	if !contains(joinEventPhases(reporter.payloads), `"reason":"SUPERCRUISE_SUSTAINED_RELEASE_BRAKE"`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignStationTargetBrakesSupercruisePulseCenterEntry(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -28, 0, 28, false),
		alignObservation("SOLID", -14, 0, 14, false),
		alignObservation("SOLID", -10, 0, 10, false),
		alignObservation("SOLID", -9, 0, 9, false),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"controlProfile": "SUPERCRUISE_ASSIST"}, caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if strings.Join(caller.controls, ",") != "YAW_LEFT,YAW_RIGHT" || len(caller.holds) != 2 || caller.holds[0] != 80 || caller.holds[1] != 300 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
	if !contains(joinEventPhases(reporter.payloads), `"reason":"CENTER_ENTRY_BRAKE"`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignStationTargetToleratesTransientMissingMarkerAfterDetection(t *testing.T) {
	missing := json.RawMessage(`{"schemaVersion":3,"target":{"detected":false,"presentation":"UNKNOWN","hemisphere":"UNKNOWN","offsetX":null,"offsetY":null,"centerDistancePixels":null,"centerZone":{"inside":null}}}`)
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 20, 4, 20.396, false),
		missing,
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) || strings.Join(caller.holdOps, ",") != "START,STOP" {
		t.Fatalf("output=%s holdOps=%v", output, caller.holdOps)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"reason":"TARGET_NOT_DETECTED_TRANSIENT"`) || !contains(joined, `"leaseState":"RELEASED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteAlignStationTargetKeepsRearTurnDirectionAcrossCenter(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", -2, -9, 9.22, false),
		alignObservation("HOLLOW", -1, -3, 3.162, true),
		alignObservation("HOLLOW", -2, -6, 6.325, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 || strings.Join(caller.holdOps, ",") != "START,RENEW,RENEW,STOP" {
		t.Fatalf("controls=%v holdOps=%v holdControls=%v", caller.controls, caller.holdOps, caller.holdControls)
	}
	for _, control := range caller.holdControls {
		if control != "YAW_LEFT" {
			t.Fatalf("holdControls=%v", caller.holdControls)
		}
	}
}

func TestEliteAlignStationTargetPitchesUpForFrontMarkerAboveCenter(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -2, -25, 25.08, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 2 || caller.controls[0] != "PITCH_UP" || caller.holds[0] != 300 || caller.controls[1] != "PITCH_DOWN" || caller.holds[1] != 100 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetUsesFinePulseAtReviewedFourteenPixels(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 0, -14, 14, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 1 || caller.controls[0] != "PITCH_UP" || caller.holds[0] != 120 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetUsesFineYawPulseInsideNearCenterBand(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -14, 0, 14, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 1 || caller.controls[0] != "YAW_LEFT" || caller.holds[0] != 120 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetEscalatesFinePulseAfterTwoNoMovementSamples(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -14, 0, 14, false),
		alignObservation("SOLID", -14, 0, 14, false),
		alignObservation("SOLID", -14, 0, 14, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{120, 120, 400}
	if len(caller.holds) != len(want) {
		t.Fatalf("holds=%v", caller.holds)
	}
	for index := range want {
		if caller.holds[index] != want[index] {
			t.Fatalf("holds=%v want=%v", caller.holds, want)
		}
	}
}

func TestEliteAlignStationTargetEscalatesMediumPulseAfterTwoNoMovementSamples(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -23, 0, 23, false),
		alignObservation("SOLID", -23, 0, 23, false),
		alignObservation("SOLID", -23, 0, 23, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{300, 300, 400}
	for index := range want {
		if caller.holds[index] != want[index] {
			t.Fatalf("holds=%v want=%v", caller.holds, want)
		}
	}
}

func TestEliteAlignStationTargetFailsAfterMeasuredNoProgress(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "no measurable Compass movement") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || strings.Join(caller.holdOps, ",") != "START,RENEW,RENEW,RENEW,STOP" {
		t.Fatalf("controls=%v holdOps=%v", caller.controls, caller.holdOps)
	}
}

func TestEliteAlignStationTargetReportsKnownEDPitchInitializationState(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 0, -20, 20, false),
		alignObservation("SOLID", 0, -20, 20, false),
		alignObservation("SOLID", 0, -20, 20, false),
		alignObservation("SOLID", 0, -20, 20, false),
		alignObservation("SOLID", 0, -20, 20, false),
	}}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "ED_PITCH_INPUT_CONTEXT_NOT_READY") || !contains(err.Error(), "power on or reconnect") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 4 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
	if len(reporter.payloads) == 0 {
		t.Fatal("expected streamed diagnostic information")
	}
	lastPayload := string(reporter.payloads[len(reporter.payloads)-1])
	if !contains(lastPayload, `"reason":"ED_PITCH_INPUT_CONTEXT_NOT_READY"`) ||
		!contains(lastPayload, `"code":"ED_PITCH_INPUT_CONTEXT_NOT_READY"`) ||
		!contains(lastPayload, `"recommendedAction":"Power on or reconnect`) {
		t.Fatalf("last payload=%s", lastPayload)
	}
}

func TestEliteAlignStationTargetTrackDoesNotInferNoProgressFromMovingTargetFrames(t *testing.T) {
	observations := make([]json.RawMessage, 10)
	for index := range observations {
		observations[index] = alignObservation("HOLLOW", -15, -4, 15.524, false)
	}
	caller := &alignStationTargetCaller{observations: observations}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{"mode": "TRACK", "trackingSamples": float64(10)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"TRACKING_WINDOW_COMPLETED"`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.controls) != 9 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetFailsAfterSustainedFrontMovingAwayTrend(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", 0, -10, 10, false),
		alignObservation("SOLID", 0, -11, 11, false),
		alignObservation("SOLID", 0, -12, 12, false),
		alignObservation("SOLID", 0, -13, 13, false),
		alignObservation("SOLID", 0, -14, 14, false),
		alignObservation("SOLID", 0, -15, 15, false),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "moved the front Compass target away") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 5 {
		t.Fatalf("controls=%v holds=%v", caller.controls, caller.holds)
	}
}

func TestEliteAlignStationTargetFailsBeforeInputWhenTargetMissing(t *testing.T) {
	missing := json.RawMessage(`{"schemaVersion":3,"target":{"detected":false,"presentation":"UNKNOWN","hemisphere":"UNKNOWN","offsetX":null,"offsetY":null,"centerDistancePixels":null,"centerZone":{"inside":null}}}`)
	caller := &alignStationTargetCaller{observations: []json.RawMessage{missing}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "establish the intended Station target lock") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("controls=%v", caller.controls)
	}
}
