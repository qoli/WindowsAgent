package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseDestinationCaller struct {
	flightStates    []string
	flightIndex     int
	compassCalls    int
	speedCalls      int
	throttles       []int
	supercruiseKeys int
	failFSD         bool
}

func (c *supercruiseDestinationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/ship-status":
		return json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`), nil
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.Marshal(map[string]any{"control": map[int64]string{-100: "SetSpeedMinus100", 0: "SetSpeedZero", 75: "SetSpeed75", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/compass":
		c.compassCalls++
		return alignObservation("SOLID", 1, 1, 1.414, true), nil
	case "elite-dangerous/ship-attitude-control":
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/supercruise-control":
		c.supercruiseKeys++
		if c.failFSD {
			return nil, errors.New("Elite Dangerous control Supercruise has no Keyboard binding")
		}
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"text":"fixture"}`), nil
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}})
	case "elite-dangerous/ship-speed":
		c.speedCalls++
		return json.RawMessage(`{"speed":{"state":"STOPPED","displayValue":0,"rawCandidate":0}}`), nil
	default:
		return nil, errors.New("unexpected supercruise child Action: " + id)
	}
}

func loadEliteSupercruiseToDestinationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-to-destination"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteSupercruiseToDestinationRequiresVisualSafeDisengageAndStop(t *testing.T) {
	caller := &supercruiseDestinationCaller{flightStates: []string{
		"FSD_CHARGING", "SUPERCRUISE", "UNKNOWN", "SAFE_DISENGAGE_READY", "SAFE_DISENGAGE_READY",
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseToDestinationPackage(t),
		map[string]any{"targetName": "NAV BEACON", "targetLocked": true, "normalSpaceConfirmed": true}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"task":"SUPERCRUISE_TO_DESTINATION"`) ||
		!contains(string(output), `"safeDisengageConfirmations":2`) ||
		!contains(string(output), `"stoppedConfirmations":3`) ||
		!contains(string(output), `"finalCommandedThrottle":0`) {
		t.Fatalf("output=%s", output)
	}
	wantThrottles := []int{0, 100, 75, 0}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v", caller.throttles)
		}
	}
	if caller.supercruiseKeys != 2 || caller.speedCalls != 3 {
		t.Fatalf("supercruiseKeys=%d speedCalls=%d", caller.supercruiseKeys, caller.speedCalls)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"phase":"SAFE_DISENGAGE_READY"`) || !contains(joined, `"phase":"COMPLETED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteSupercruiseToDestinationDoesNotFallbackFromMissingDedicatedBinding(t *testing.T) {
	caller := &supercruiseDestinationCaller{failFSD: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseToDestinationPackage(t),
		map[string]any{"targetName": "NAV BEACON", "targetLocked": true, "normalSpaceConfirmed": true}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "Supercruise has no Keyboard binding") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseKeys != 1 {
		t.Fatalf("supercruiseKeys=%d", caller.supercruiseKeys)
	}
	// The failure compensation is the only command after the initial explicit
	// stop. No combined FSD Action exists to call as a substitute.
	if len(caller.throttles) != 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}
