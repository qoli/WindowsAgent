package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type fixedSupercruiseSphereSeparationCaller struct {
	spheres       []map[string]any
	sphereIndex   int
	statusIndex   int
	overheatAt    int
	chargeAt      int
	leaveCruiseAt int
	controls      []string
	vectorOps     []string
	throttles     []int
}

func (c *fixedSupercruiseSphereSeparationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/filesystem/status":
		callIndex := c.statusIndex
		c.statusIndex++
		flags := int64(16)
		flags2 := int64(0)
		if c.overheatAt >= 0 && callIndex == c.overheatAt {
			flags += 1048576
		}
		if c.chargeAt >= 0 && callIndex == c.chargeAt {
			flags += 131072
		}
		if c.leaveCruiseAt >= 0 && callIndex == c.leaveCruiseAt {
			flags -= 16
		}
		return json.Marshal(map[string]any{
			"state":     "AVAILABLE",
			"freshness": "CURRENT",
			"source":    map[string]any{"sourceTimestamp": "status-" + string(rune('A'+callIndex))},
			"data":      map[string]any{"Flags": flags, "Flags2": flags2},
		})
	case "elite-dangerous/supercruise-sphere-direction":
		if c.sphereIndex >= len(c.spheres) {
			return nil, errors.New("unexpected sphere observation")
		}
		result := c.spheres[c.sphereIndex]
		c.sphereIndex++
		return json.Marshal(result)
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/ship-attitude-control":
		control := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.Marshal(map[string]any{"control": control, "holdMs": inputs["holdMs"]})
	case "elite-dangerous/ship-attitude-vector-hold":
		operation := inputs["operation"].(string)
		control := inputs["control"].(string)
		c.vectorOps = append(c.vectorOps, operation+":"+control)
		if operation == "START" {
			c.controls = append(c.controls, control)
			return json.RawMessage(`{"operation":"STARTED","control":"PITCH_UP_YAW_RIGHT","leaseId":"fixture-lease"}`), nil
		}
		return json.Marshal(map[string]any{"operation": "STOPPED", "control": control, "leaseId": inputs["leaseId"]})
	default:
		return nil, errors.New("unexpected fixed sphere-separation child Action: " + id)
	}
}

func loadEliteFixedSupercruiseSphereSeparationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "fixed-supercruise-sphere-separation"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func fixedSeparationDetectedSphere(control string) map[string]any {
	return map[string]any{
		"sphere": map[string]any{
			"state": "DETECTED", "centerX": 1100.0, "centerY": 500.0,
			"radiusPixels": 500.0, "signedLimbClearancePixels": -120.0,
			"confidencePermille": int64(850),
		},
		"direction": map[string]any{"state": "READY", "control": control, "reason": "fixture"},
	}
}

func fixedSeparationAbsentSphere() map[string]any {
	return map[string]any{
		"sphere": map[string]any{
			"state": "ABSENT", "centerX": nil, "centerY": nil,
			"radiusPixels": nil, "signedLimbClearancePixels": nil,
			"confidencePermille": int64(0),
		},
		"direction": map[string]any{"state": "UNKNOWN", "control": nil, "reason": "fixture absent"},
	}
}

func newFixedSeparationCaller(spheres ...map[string]any) *fixedSupercruiseSphereSeparationCaller {
	return &fixedSupercruiseSphereSeparationCaller{
		spheres: spheres, overheatAt: -1, chargeAt: -1, leaveCruiseAt: -1,
	}
}

func TestEliteFixedSupercruiseSphereSeparationExecutesOneFixedMechanicalSegment(t *testing.T) {
	caller := newFixedSeparationCaller(
		fixedSeparationDetectedSphere("PITCH_UP_YAW_RIGHT"),
		fixedSeparationDetectedSphere("PITCH_UP_YAW_RIGHT"),
	)
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteFixedSupercruiseSphereSeparationPackage(t), map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"completed":true`, `"directionConfirmations":2`, `"turnPulses":8`, `"fixedTurnDurationMs":6400`, `"separationDurationMs":30000`, `"separationSamples":60`, `"finalStatusConfirmations":2`, `"finalCommandedThrottle":0`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.sphereIndex != 2 {
		t.Fatalf("sphere observations=%d want=2", caller.sphereIndex)
	}
	if caller.statusIndex != 63 {
		t.Fatalf("Status observations=%d want=63", caller.statusIndex)
	}
	if len(caller.controls) != 8 {
		t.Fatalf("controls=%v", caller.controls)
	}
	if len(caller.vectorOps) != 16 {
		t.Fatalf("vector operations=%v", caller.vectorOps)
	}
	for index := 0; index < len(caller.vectorOps); index += 2 {
		if caller.vectorOps[index] != "START:PITCH_UP_YAW_RIGHT" || caller.vectorOps[index+1] != "STOP:PITCH_UP_YAW_RIGHT" {
			t.Fatalf("vector operations=%v", caller.vectorOps)
		}
	}
	if got := caller.throttles; len(got) != 3 || got[0] != 0 || got[1] != 100 || got[2] != 0 {
		t.Fatalf("throttles=%v", got)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"VERIFYING_SUPERCRUISE", "CONFIRMING_OUTWARD_DIRECTION", "EXECUTING_FIXED_OUTWARD_TURN", "SEPARATION_FLIGHT", "VERIFYING_FINAL_STATUS", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing phase %s events=%s", phase, joined)
		}
	}
}

func TestEliteFixedSupercruiseSphereSeparationFailsStoppedWhenDirectionIsAbsent(t *testing.T) {
	caller := newFixedSeparationCaller(fixedSeparationAbsentSphere())
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteFixedSupercruiseSphereSeparationPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FIXED_SPHERE_SEPARATION_DIRECTION_UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || containsInt(caller.throttles, 100) || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("controls=%v throttles=%v", caller.controls, caller.throttles)
	}
}

func TestEliteFixedSupercruiseSphereSeparationRejectsDirectionDisagreementBeforeInput(t *testing.T) {
	caller := newFixedSeparationCaller(
		fixedSeparationDetectedSphere("YAW_RIGHT"),
		fixedSeparationDetectedSphere("PITCH_UP"),
	)
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteFixedSupercruiseSphereSeparationPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FIXED_SPHERE_SEPARATION_DIRECTION_NOT_STABLE") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || containsInt(caller.throttles, 100) || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("controls=%v throttles=%v", caller.controls, caller.throttles)
	}
}

func TestEliteFixedSupercruiseSphereSeparationStopsOnUnsafeSeparationStatus(t *testing.T) {
	caller := newFixedSeparationCaller(
		fixedSeparationDetectedSphere("YAW_RIGHT"),
		fixedSeparationDetectedSphere("YAW_RIGHT"),
	)
	caller.overheatAt = 3
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteFixedSupercruiseSphereSeparationPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FIXED_SPHERE_SEPARATION_OVERHEATING:SEPARATION_FLIGHT") {
		t.Fatalf("error=%v", err)
	}
	if !containsInt(caller.throttles, 100) || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
