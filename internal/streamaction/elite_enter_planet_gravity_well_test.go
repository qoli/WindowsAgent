package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type enterPlanetGravityWellCaller struct {
	targetName             string
	escapeProbes           []bool
	probeIndex             int
	activeProbeEscape      bool
	charging               bool
	supercruise            bool
	throttle               int64
	toggles                int
	throttles              []int64
	distances              []float64
	distanceIndex          int
	alignCompass           int
	alignVisible           int
	alignVisibleInputs     []map[string]any
	flightCalls            int
	autoDropAfterDistances int
}

func (c *enterPlanetGravityWellCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/set-throttle":
		percent := inputs["percent"].(int64)
		c.throttle = percent
		c.throttles = append(c.throttles, percent)
		if percent == 100 && c.charging && !c.activeProbeEscape {
			c.charging = false
			c.supercruise = true
		}
		return json.RawMessage(`{"control":"SetSpeed"}`), nil
	case "elite-dangerous/supercruise-control":
		c.toggles++
		if c.supercruise {
			c.supercruise = false
			c.charging = false
		} else if c.charging {
			c.charging = false
		} else {
			c.charging = true
			c.activeProbeEscape = false
			if c.probeIndex < len(c.escapeProbes) {
				c.activeProbeEscape = c.escapeProbes[c.probeIndex]
			}
			c.probeIndex++
		}
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	case "elite-dangerous/filesystem/status":
		if c.supercruise && c.autoDropAfterDistances > 0 && c.distanceIndex >= c.autoDropAfterDistances {
			c.supercruise = false
		}
		flags := int64(8)
		if c.supercruise {
			flags += 16
		}
		if c.charging {
			flags += 131072
		}
		return json.Marshal(map[string]any{
			"state": "AVAILABLE", "freshness": "CURRENT",
			"source": map[string]any{"sourceTimestamp": "2026-08-12T00:00:00Z"},
			"data": map[string]any{
				"Flags": flags, "Flags2": int64(0),
				"Destination": map[string]any{"Name": c.targetName},
			},
		})
	case "elite-dangerous/ship-heat":
		return json.RawMessage(`{"heat":{"state":"KNOWN","percent":25}}`), nil
	case "elite-dangerous/ship-status":
		return json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`), nil
	case "elite-dangerous/flight-prompt-text":
		return nil, errors.New("workflow bypassed public flight-status Action")
	case "elite-dangerous/flight-status":
		c.flightCalls++
		state := "FSD_THROTTLE_UP_REQUIRED"
		text := "THROTTLE UP TO ENGAGE"
		if c.activeProbeEscape && c.charging {
			state = "FSD_ESCAPE_VECTOR_REQUIRED"
			text = "ALIGN WITH ESCAPE VECTOR"
		} else if c.supercruise {
			state = "SUPERCRUISE"
			text = "SUPERCRUISE"
		}
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state, "known": true}, "source": map[string]any{"text": text}})
	case "elite-dangerous/align-station-target":
		c.alignCompass++
		return json.RawMessage(`{"schemaVersion":1,"task":"ALIGN_STATION_TARGET","completed":true}`), nil
	case "elite-dangerous/align-visible-target":
		c.alignVisible++
		c.alignVisibleInputs = append(c.alignVisibleInputs, inputs)
		return json.RawMessage(`{"schemaVersion":1,"task":"ALIGN_VISIBLE_TARGET","completed":true}`), nil
	case "elite-dangerous/request-docking-range":
		if c.distanceIndex >= len(c.distances) {
			return nil, errors.New("distance fixture exhausted")
		}
		distance := c.distances[c.distanceIndex]
		c.distanceIndex++
		return json.Marshal(map[string]any{"requestDockingRange": map[string]any{
			"state": "DENIED", "distanceMeters": distance,
			"evidence": map[string]any{"reason": "TEST_TARGET_DISTANCE"},
		}})
	case "elite-dangerous/ship-speed-text-regions":
		return json.RawMessage(`{"regions":[{"text":"18.0 km/s","detectionConfidence":0.99,"recognitionConfidence":0.99}]}`), nil
	default:
		return nil, errors.New("unexpected enter-gravity-well child Action: " + id)
	}
}

func loadEliteEnterPlanetGravityWellPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "enter-planet-gravity-well"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteEnterPlanetGravityWellCompletesWithoutMovementWhenEscapeVectorAlreadyExists(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{targetName: "LTT 11244 A 2", escapeProbes: []bool{true}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": caller.targetName}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"completed":true`, `"entryMode":"ALREADY_IN_GRAVITY_WELL"`, `"escapeVectorConfirmations":2`, `"approachSampleCount":0`, `"finalCommandedThrottle":0`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.alignCompass != 0 || caller.alignVisible != 0 {
		t.Fatalf("already-present gravity well must not align or move, compass=%d visible=%d", caller.alignCompass, caller.alignVisible)
	}
	if caller.toggles != 2 || !equalInt64s(caller.throttles, []int64{0, 0, 0}) {
		t.Fatalf("toggles=%d throttles=%v", caller.toggles, caller.throttles)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"PREFLIGHT", "PROBING_CURRENT_POSITION", "CANCELLING_PROBE", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing phase %s events=%s", phase, joined)
		}
	}
}

func TestEliteEnterPlanetGravityWellApproachesDropsAndRequiresSecondEscapeVectorProof(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{
		targetName:   "LTT 11244 A 2",
		escapeProbes: []bool{false, false, true},
		distances:    []float64{19_000_000, 18_500_000, 18_000_000},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": caller.targetName}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("%v events=%s", err, joinEventPhases(reporter.payloads))
	}
	for _, expected := range []string{`"entryMode":"APPROACHED_AND_VERIFIED"`, `"approachSampleCount":3`, `"finalTargetDistanceMeters":18000000`, `"finalSupercruiseSpeedMetersPerSecond":18000`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.alignCompass != 1 || caller.alignVisible != 1 {
		t.Fatalf("alignment calls compass=%d visible=%d", caller.alignCompass, caller.alignVisible)
	}
	if len(caller.alignVisibleInputs) != 1 || caller.alignVisibleInputs[0]["centerHintConfirmed"] != true || caller.alignVisibleInputs[0]["confirmedHintProfile"] != "SUPERCRUISE_ASSIST" {
		t.Fatalf("visible alignment inputs=%v", caller.alignVisibleInputs)
	}
	if caller.toggles != 6 {
		t.Fatalf("toggles=%d, want initial probe start/cancel, entry/drop, final probe start/cancel", caller.toggles)
	}
	if caller.flightCalls != 5 {
		t.Fatalf("flight OCR calls=%d, want two non-escape confirmations, one entry observation, and two escape confirmations", caller.flightCalls)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"ALIGNING", "ENTERING_SUPERCRUISE", "APPROACHING", "DROPPING", "VERIFYING_GRAVITY_WELL", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing phase %s events=%s", phase, joined)
		}
	}
}

func TestEliteEnterPlanetGravityWellTakesOverExistingSupercruiseApproach(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{
		targetName:   "LTT 11244 A 2",
		supercruise:  true,
		escapeProbes: []bool{true},
		distances:    []float64{19_000_000, 18_500_000, 18_000_000},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": caller.targetName}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("%v events=%s", err, joinEventPhases(reporter.payloads))
	}
	for _, expected := range []string{`"entryMode":"SUPERCRUISE_HANDOFF_APPROACHED_AND_VERIFIED"`, `"approachSampleCount":3`, `"escapeVectorConfirmations":2`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.alignCompass != 0 || caller.alignVisible != 0 {
		t.Fatalf("Supercruise handoff must not repeat alignment, compass=%d visible=%d", caller.alignCompass, caller.alignVisible)
	}
	if caller.toggles != 3 {
		t.Fatalf("toggles=%d, want only manual drop and final probe start/cancel", caller.toggles)
	}
	if caller.flightCalls != 2 {
		t.Fatalf("flight OCR calls=%d, want only final Escape Vector confirmations", caller.flightCalls)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"SUPERCRUISE_TARGET_AND_SHIP_STATUS_CONFIRMED"`) ||
		!contains(joined, `"reason":"ALREADY_IN_SUPERCRUISE_APPROACH"`) ||
		contains(joined, `"phase":"PROBING_CURRENT_POSITION"`) ||
		contains(joined, `"phase":"ALIGNING"`) ||
		contains(joined, `"phase":"ENTERING_SUPERCRUISE"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteEnterPlanetGravityWellStopsWhenTargetDistanceKeepsIncreasing(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{
		targetName:  "LTT 11244 A 2",
		supercruise: true,
		distances:   []float64{20_000_000, 21_000_000, 22_000_000, 23_000_000},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": caller.targetName}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "distance increased for three consecutive") {
		t.Fatalf("err=%v events=%s", err, joinEventPhases(reporter.payloads))
	}
	if caller.throttle != 0 {
		t.Fatalf("wrong-direction failure left throttle=%d", caller.throttle)
	}
	if caller.toggles != 0 || caller.alignCompass != 0 || caller.alignVisible != 0 {
		t.Fatalf("wrong-direction handoff must stop without toggles or alignment: toggles=%d compass=%d visible=%d", caller.toggles, caller.alignCompass, caller.alignVisible)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"commandedThrottle":0`) || !contains(joined, `"reason":"TARGET_DISTANCE_INCREASING_LIMIT_REACHED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteEnterPlanetGravityWellRejectsMismatchedStatusDestinationBeforeProbe(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{targetName: "Different Body"}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "Destination does not match") {
		t.Fatalf("err=%v", err)
	}
	if caller.toggles != 0 {
		t.Fatalf("preflight mismatch must not toggle FSD, got %d", caller.toggles)
	}
}

func TestEliteEnterPlanetGravityWellTreatsGameAutomaticDropAsVerificationHandoff(t *testing.T) {
	caller := &enterPlanetGravityWellCaller{
		targetName:             "LTT 11244 A 2",
		escapeProbes:           []bool{false, false, true},
		distances:              []float64{8_000_000, 7_760_000},
		autoDropAfterDistances: 2,
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteEnterPlanetGravityWellPackage(t), map[string]any{"targetName": caller.targetName}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("%v events=%s", err, joinEventPhases(reporter.payloads))
	}
	if !contains(string(output), `"entryMode":"APPROACHED_AND_VERIFIED"`) ||
		!contains(string(output), `"approachSampleCount":3`) ||
		!contains(string(output), `"finalTargetDistanceMeters":7760000`) {
		t.Fatalf("output=%s", output)
	}
	if caller.distanceIndex != 2 {
		t.Fatalf("distance OCR calls=%d, must stop when automatic drop removes Supercruise HUD", caller.distanceIndex)
	}
	if caller.toggles != 5 {
		t.Fatalf("toggles=%d, automatic drop must replace the manual drop toggle", caller.toggles)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"GAME_AUTOMATIC_GRAVITY_WELL_DROP_CONFIRMED"`) ||
		!contains(joined, `"phase":"VERIFYING_GRAVITY_WELL"`) ||
		!contains(joined, `"phase":"COMPLETED"`) {
		t.Fatalf("events=%s", joined)
	}
}
