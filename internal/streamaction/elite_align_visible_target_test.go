package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type alignVisibleTargetCaller struct {
	heats           []json.RawMessage
	positions       []json.RawMessage
	flightStatuses  []json.RawMessage
	posErrors       []error
	controls        []string
	positionActions []string
	positionInputs  []map[string]any
	heatIndex       int
	posIndex        int
	flightIndex     int
}

func (c *alignVisibleTargetCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/ship-heat":
		if c.heatIndex >= len(c.heats) {
			return nil, errors.New("unexpected heat observation")
		}
		value := c.heats[c.heatIndex]
		c.heatIndex++
		return value, nil
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStatuses) {
			return nil, errors.New("unexpected flight-status observation")
		}
		value := c.flightStatuses[c.flightIndex]
		c.flightIndex++
		return value, nil
	case "elite-dangerous/escape-vector-visible-position", "elite-dangerous/supercruise-target-position", "elite-dangerous/supercruise-visible-reticle-position":
		c.positionActions = append(c.positionActions, id)
		c.positionInputs = append(c.positionInputs, inputs)
		if len(c.posErrors) > 0 {
			err := c.posErrors[0]
			c.posErrors = c.posErrors[1:]
			return nil, err
		}
		if c.posIndex >= len(c.positions) {
			return nil, errors.New("unexpected position observation")
		}
		value := c.positions[c.posIndex]
		c.posIndex++
		return value, nil
	case "elite-dangerous/ship-attitude-control":
		control := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.Marshal(map[string]any{"control": control})
	default:
		return nil, errors.New("unexpected align-visible-target child Action: " + id)
	}
}

func TestEliteAlignVisibleTargetAcquiresIdentityThenTracksReticle(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(8, 6, 10),
			visiblePosition(7, 5, 8.6),
			visiblePosition(6, 4, 7.3),
			visiblePosition(5, 3, 5.8),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LP 298-42", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	want := []string{
		"elite-dangerous/supercruise-target-position",
		"elite-dangerous/supercruise-visible-reticle-position",
		"elite-dangerous/supercruise-visible-reticle-position",
		"elite-dangerous/supercruise-visible-reticle-position",
	}
	if len(caller.positionActions) != len(want) {
		t.Fatalf("position Actions=%v want=%v", caller.positionActions, want)
	}
	for index := range want {
		if caller.positionActions[index] != want[index] {
			t.Fatalf("position Actions=%v want=%v", caller.positionActions, want)
		}
		if caller.positionActions[index] == "elite-dangerous/supercruise-target-position" && caller.positionInputs[index]["reticleEvidencePolicy"] != "HUD_OVERLAY_AWARE" {
			t.Fatalf("identity inputs=%v", caller.positionInputs[index])
		}
		if caller.positionActions[index] == "elite-dangerous/supercruise-visible-reticle-position" && caller.positionInputs[index]["evidencePolicy"] != "HUD_OVERLAY_AWARE" {
			t.Fatalf("tracking inputs=%v", caller.positionInputs[index])
		}
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `"observationMode":"IDENTITY_ACQUISITION"`) ||
		strings.Count(events, `"observationMode":"RETICLE_TRACKING"`) != 4 {
		t.Fatalf("events=%s", events)
	}
}

func TestEliteAlignVisibleTargetCompletesFromConcurrentBlueZoneGateDuringUnknownHeat(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		flightStatuses: []json.RawMessage{
			visibleFlightStatus("UNKNOWN", "MOVE THROTTLE TO BLUE ZONE"),
			visibleFlightStatus("UNKNOWN", "MOVE THROTTLE TO BLUE ZONE"),
		},
	}
	for index := 0; index < 8; index++ {
		caller.heats = append(caller.heats, visibleHeat("UNKNOWN", nil))
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "DROWNED RAT ORBITAL", "stopBeforeAlign": false, "centerHintConfirmed": true, "blueZoneGateEnabled": true, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completionEvidence":"BLUE_ZONE_GAME_ALIGNMENT_CONFIRMED"`) || !contains(string(output), `"blueZoneConfirmations":2`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.heatIndex != 8 || caller.flightIndex != 2 || len(caller.positionActions) != 0 || len(caller.controls) != 0 {
		t.Fatalf("heat=%d flight=%d positions=%v controls=%v", caller.heatIndex, caller.flightIndex, caller.positionActions, caller.controls)
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `BLUE_ZONE_ALIGNMENT_GATE_1_OF_2`) || !contains(events, `BLUE_ZONE_ALIGNMENT_GATE_2_OF_2`) || !contains(events, `"phase":"COMPLETED"`) {
		t.Fatalf("events=%s", events)
	}
}

func TestEliteAlignVisibleTargetDoesNotCompleteFromInterruptedBlueZoneGate(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		flightStatuses: []json.RawMessage{
			visibleFlightStatus("UNKNOWN", "MOVE THROTTLE TO BLUE ZONE"),
			visibleFlightStatus("UNKNOWN", ""),
		},
	}
	for index := 0; index < 8; index++ {
		caller.heats = append(caller.heats, visibleHeat("UNKNOWN", nil))
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "DROWNED RAT ORBITAL", "stopBeforeAlign": false, "centerHintConfirmed": true, "blueZoneGateEnabled": true, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "safe strict heat checkpoint") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.positionActions) != 0 || len(caller.controls) != 0 {
		t.Fatalf("interrupted Gate authorized alignment: positions=%v controls=%v", caller.positionActions, caller.controls)
	}
}

func TestEliteAlignVisibleTargetPollsBlueZoneWhileCVKeepsAligning(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		flightStatuses: []json.RawMessage{
			visibleFlightStatus("UNKNOWN", "MOVE THROTTLE TO BLUE ZONE"),
			visibleFlightStatus("UNKNOWN", "MOVE THROTTLE TO BLUE ZONE"),
		},
		positions: []json.RawMessage{
			visiblePositionWithPresentation(35, -40, 53.2, "DASHED"),
			visiblePositionWithPresentation(29, -34, 44.7, "DASHED"),
		},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "DROWNED RAT ORBITAL", "stopBeforeAlign": false, "centerHintConfirmed": true, "blueZoneGateEnabled": true, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, &fixtureReporter{})
	if err != nil || !contains(string(output), `"completionEvidence":"BLUE_ZONE_GAME_ALIGNMENT_CONFIRMED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 2 || caller.flightIndex != 2 || caller.posIndex != 2 {
		t.Fatalf("controls=%v flight=%d positions=%d", caller.controls, caller.flightIndex, caller.posIndex)
	}
}

func TestEliteAlignVisibleTargetAcceptsIdentityBoundDashedReticle(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePositionWithPresentation(9, 6, 10.8, "DASHED"),
			visiblePositionWithPresentation(8, 5, 9.4, "DASHED"),
			visiblePositionWithPresentation(7, 4, 8.1, "DASHED"),
			visiblePositionWithPresentation(6, 3, 6.7, "DASHED"),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "ARIETIS SECTOR LO-F A12-1", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) || !contains(string(output), `"presentation":"DASHED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("already-centred dashed reticle authorized controls: %v", caller.controls)
	}
	if strings.Count(joinEventPhases(reporter.payloads), `"presentation":"DASHED"`) != 5 {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetUsesConfirmedCompassCenterAsFreshReticleHint(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePositionWithPresentation(9, -6, 10.8, "DASHED"),
			visiblePositionWithPresentation(8, -5, 9.4, "DASHED"),
			visiblePositionWithPresentation(7, -4, 8.1, "DASHED"),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "DROWNED RAT ORBITAL", "stopBeforeAlign": false, "centerHintConfirmed": true, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) || !contains(string(output), `"presentation":"DASHED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.positionActions) != 3 {
		t.Fatalf("position Actions=%v", caller.positionActions)
	}
	for index, actionID := range caller.positionActions {
		if actionID != "elite-dangerous/supercruise-visible-reticle-position" {
			t.Fatalf("confirmed centre called %s at %d; actions=%v", actionID, index, caller.positionActions)
		}
	}
	if caller.positionInputs[0]["hintX"].(int64) != 960 || caller.positionInputs[0]["hintY"].(int64) != 540 {
		t.Fatalf("first centre hint inputs=%v", caller.positionInputs[0])
	}
	if contains(joinEventPhases(reporter.payloads), `"observationMode":"IDENTITY_ACQUISITION"`) {
		t.Fatalf("confirmed centre unexpectedly repeated identity acquisition: %s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetRetriesConfirmedLocalHintWithoutIdentityFallback(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			unknownVisiblePosition(),
			visiblePositionWithPresentation(9, -6, 10.8, "DASHED"),
			visiblePositionWithPresentation(8, -5, 9.4, "DASHED"),
			visiblePositionWithPresentation(7, -4, 8.1, "DASHED"),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "DROWNED RAT ORBITAL", "stopBeforeAlign": false, "centerHintConfirmed": true, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	for index, actionID := range caller.positionActions {
		if actionID != "elite-dangerous/supercruise-visible-reticle-position" {
			t.Fatalf("confirmed hint fell back to %s at %d; actions=%v", actionID, index, caller.positionActions)
		}
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `"reason":"RETICLE_TRACKING_LOST_RETRY_CONFIRMED_HINT"`) || contains(events, `"observationMode":"IDENTITY_ACQUISITION"`) {
		t.Fatalf("events=%s", events)
	}
}

func TestEliteAlignVisibleTargetKeepsContinuousReticleTrackThroughPillarOcclusion(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 46), visibleHeat("KNOWN", 46)},
		positions: []json.RawMessage{
			visiblePosition(320, -120, 341.8),
			visiblePosition(280, -100, 297.3),
			visiblePosition(240, -80, 253.0),
			visiblePosition(200, -60, 208.8),
			visiblePosition(160, -40, 164.9),
			visiblePosition(120, -20, 121.7),
			visiblePosition(80, -10, 80.6),
			visiblePosition(40, -8, 40.8),
			visiblePosition(18, -4, 18.4),
			visiblePosition(8, -4, 8.9),
			visiblePosition(7, -3, 7.6),
			visiblePosition(6, -2, 6.3),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "Aasgananu", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	identityCalls := 0
	for _, actionID := range caller.positionActions {
		if actionID == "elite-dangerous/supercruise-target-position" {
			identityCalls++
		}
	}
	if identityCalls != 1 {
		t.Fatalf("continuous reticle track unnecessarily reacquired occluded identity: actions=%v", caller.positionActions)
	}
}

func TestEliteAlignVisibleTargetAllowsMoreThanEightyConvergingMicroPulses(t *testing.T) {
	caller := &alignVisibleTargetCaller{}
	for index := 0; index < 100; index++ {
		caller.heats = append(caller.heats, visibleHeat("KNOWN", 24))
	}
	caller.positions = append(caller.positions, visiblePosition(80, -400, 407.9))
	for index := 0; index < 140; index++ {
		caller.positions = append(caller.positions, visiblePosition(80, -400+float64(index), 400))
	}
	for index := 0; index < 10; index++ {
		caller.positions = append(caller.positions, visiblePosition(8, 6, 10))
	}

	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "OBAMA REACH", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, &fixtureReporter{})
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) <= 120 || len(caller.controls) > 160 {
		t.Fatalf("controls=%d want >120 and <=160", len(caller.controls))
	}
}

func TestEliteAlignVisibleTargetTrackingLossReacquiresIdentityWithoutSteering(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(0, -30, 30),
			unknownVisiblePosition(),
			visiblePosition(8, 6, 10),
			visiblePosition(7, 5, 8.6),
			visiblePosition(6, 4, 7.3),
			visiblePosition(5, 3, 5.8),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LP 298-42", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("tracking loss authorized control: %v", caller.controls)
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `RETICLE_TRACKING_LOST_REACQUIRE_IDENTITY`) ||
		strings.Count(events, `"observationMode":"IDENTITY_ACQUISITION"`) != 2 {
		t.Fatalf("events=%s", events)
	}
}

func transientWGCRegionError() error {
	return errors.New("capture OCR text regions Action region: persistent WGC worker region capture: persistent region capture failed: failed to create the region unordered-access view: COM method 8: HRESULT 0x80070057")
}

func loadEliteAlignVisibleTargetPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "align-visible-target"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func visibleHeat(state string, percent any) json.RawMessage {
	reason := "RAW_PERCENT_TEXT_CONFIRMED"
	if state == "UNKNOWN" {
		reason = "RAW_PERCENT_TEXT_NOT_CONFIRMED"
	}
	value, _ := json.Marshal(map[string]any{"heat": map[string]any{
		"state": state, "percent": percent, "evidence": map[string]any{"reason": reason},
	}})
	return value
}

func visibleFlightStatus(state, text string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"flightStatus": map[string]any{"state": state},
		"source":       map[string]any{"text": text},
	})
	return value
}

func visiblePosition(x, y, distance float64) json.RawMessage {
	return visiblePositionWithPresentation(x, y, distance, "SOLID")
}

func visiblePositionWithPresentation(x, y, distance float64, presentation string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "target": map[string]any{
		"state": "DETECTED", "referenceX": 960 + x, "referenceY": 540 + y,
		"offsetX": x, "offsetY": y, "centerDistancePixels": distance,
		"reason": "TEST", "presentation": presentation, "occupiedAngularBins": 24, "angularRuns": 8,
		"rawTexts": []string{"ESCAPE", "VECTOR"},
	}, "timing": map[string]any{}, "evidence": map[string]any{"capturedAt": "2026-08-13T01:02:03Z"}})
	return value
}

func unknownVisiblePosition() json.RawMessage {
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "target": map[string]any{
		"state": "UNKNOWN", "referenceX": nil, "referenceY": nil,
		"offsetX": nil, "offsetY": nil, "centerDistancePixels": nil,
		"reason": "TARGET_TEXT_NOT_FOUND", "rawTexts": []string{},
	}, "timing": map[string]any{}, "evidence": map[string]any{"capturedAt": "2026-08-13T01:02:03Z"}})
	return value
}

func TestEliteAlignVisibleTargetDoesNotSteerFromUnknownDestination(t *testing.T) {
	caller := &alignVisibleTargetCaller{}
	for index := 0; index < 8; index++ {
		caller.heats = append(caller.heats, visibleHeat("KNOWN", 23))
		caller.positions = append(caller.positions, unknownVisiblePosition())
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err == nil || !contains(err.Error(), "bounded observation window") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("UNKNOWN target authorized controls: %v", caller.controls)
	}
	if caller.heatIndex != 1 {
		t.Fatalf("destination UNKNOWN window used %d heat calls, want one checkpoint", caller.heatIndex)
	}
	events := joinEventPhases(reporter.payloads)
	if contains(events, `"phase":"SEARCHING"`) || contains(events, `"command":"YAW_`) {
		t.Fatalf("UNKNOWN target emitted a search command: %s", events)
	}
}

func TestEliteAlignVisibleTargetSkipsOneTransientWGCRegionCaptureFailure(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats:     []json.RawMessage{visibleHeat("KNOWN", 23)},
		posErrors: []error{transientWGCRegionError()},
		positions: []json.RawMessage{visiblePosition(8, 6, 10), visiblePosition(8, 6, 10), visiblePosition(8, 6, 10), visiblePosition(8, 6, 10)},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "METZILI", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if !contains(joinEventPhases(reporter.payloads), `TARGET_POSITION_WGC_CAPTURE_RETRY`) || len(caller.controls) != 0 {
		t.Fatalf("events=%s controls=%v", joinEventPhases(reporter.payloads), caller.controls)
	}
}

func TestEliteAlignVisibleTargetFailsSixthTransientWGCRegionCaptureFailure(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		posErrors: []error{
			transientWGCRegionError(), transientWGCRegionError(), transientWGCRegionError(),
			transientWGCRegionError(), transientWGCRegionError(), transientWGCRegionError(),
		},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "METZILI", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err == nil || !contains(err.Error(), "error limit exceeded after five skipped errors") {
		t.Fatalf("error=%v", err)
	}
	if strings.Count(joinEventPhases(reporter.payloads), `TARGET_POSITION_WGC_CAPTURE_RETRY`) != 6 || len(caller.controls) != 0 {
		t.Fatalf("events=%s controls=%v", joinEventPhases(reporter.payloads), caller.controls)
	}
}

func TestEliteAlignVisibleTargetUsesRaisedMidFineDestinationPulse(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(0, -30, 30),
			visiblePosition(0, -30, 30),
			visiblePosition(0, -10, 10),
			visiblePosition(0, -9, 9),
			visiblePosition(0, -8, 8),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if !contains(joinEventPhases(reporter.payloads), `"commandHoldMs":120`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetConfirmsFreshTrackAndCapsCoarsePulseToTrackerDomain(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 24)},
		positions: []json.RawMessage{
			visiblePosition(-54, -173, 181.2), // identity hint only
			visiblePosition(-53, -171, 179.0), // fresh local track authorizes control
			visiblePosition(-40, -140, 145.6),
			visiblePosition(8, 6, 10),
			visiblePosition(7, 5, 8.6),
			visiblePosition(6, 4, 7.3),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "OBAMA REACH", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) < 1 || caller.controls[0] != "PITCH_UP" {
		t.Fatalf("controls=%v", caller.controls)
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `IDENTITY_ACQUIRED_AWAITING_FRESH_RETICLE_TRACK`) ||
		!contains(events, `"observationMode":"RETICLE_TRACKING"`) ||
		!contains(events, `"commandHoldMs":120`) {
		t.Fatalf("events=%s", events)
	}
	if len(caller.positionActions) < 2 || caller.positionActions[0] != "elite-dangerous/supercruise-target-position" || caller.positionActions[1] != "elite-dangerous/supercruise-visible-reticle-position" {
		t.Fatalf("position actions=%v", caller.positionActions)
	}
}

func TestEliteAlignVisibleTargetUsesRaisedNearDestinationYawPulse(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(15, 6, 16.2),
			visiblePosition(15, 6, 16.2),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `"command":"YAW_RIGHT"`) || !contains(events, `"commandHoldMs":120`) {
		t.Fatalf("events=%s", events)
	}
}

func TestEliteAlignVisibleTargetObservesDestinationBoundaryJitterWithoutSteering(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(15, 6, 16.2),
			visiblePosition(15, 6, 16.2),
			visiblePosition(8, 6, 10),
			visiblePosition(11, 7, 13.1),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 1 {
		t.Fatalf("boundary jitter authorized controls: %v", caller.controls)
	}
	events := joinEventPhases(reporter.payloads)
	if !contains(events, `CENTER_BOUNDARY_JITTER_TOLERATED`) {
		t.Fatalf("events=%s", events)
	}
}

func TestEliteAlignVisibleTargetAllowsTwoBoundarySamplesOnlyAfterEnteringGate(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(15, 6, 16.2),
			visiblePosition(15, 6, 16.2),
			visiblePosition(8, 6, 10),
			visiblePosition(11, 7, 13.1),
			visiblePosition(10, 8, 12.8),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 1 {
		t.Fatalf("two post-Gate boundary samples authorized controls: %v", caller.controls)
	}
	if strings.Count(joinEventPhases(reporter.payloads), `CENTER_BOUNDARY_JITTER_TOLERATED`) != 2 {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetDoesNotTolerateBoundaryBeforeEnteringGate(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
			visiblePosition(11, 7, 13.1),
			visiblePosition(11, 7, 13.1),
			visiblePosition(10, 8, 12.8),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.controls) != 2 {
		t.Fatalf("pre-Gate boundary samples were tolerated: %v", caller.controls)
	}
	if contains(joinEventPhases(reporter.payloads), `CENTER_BOUNDARY_JITTER_TOLERATED`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetRecoversAfterFiveUnknownDestinationHeatSamples(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{
			visibleHeat("UNKNOWN", nil),
			visibleHeat("UNKNOWN", nil),
			visibleHeat("UNKNOWN", nil),
			visibleHeat("UNKNOWN", nil),
			visibleHeat("UNKNOWN", nil),
			visibleHeat("KNOWN", 23),
		},
		positions: []json.RawMessage{
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
			visiblePosition(8, 6, 10),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false, "positionSource": "DESTINATION", "heatPolicy": "STRICT",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	events := joinEventPhases(reporter.payloads)
	if strings.Count(events, `STRICT_HEAT_CHECKPOINT_UNKNOWN`) != 5 || !contains(events, `"heatReason":"RAW_PERCENT_TEXT_NOT_CONFIRMED"`) {
		t.Fatalf("events=%s", events)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("UNKNOWN checkpoint authorized controls: %v", caller.controls)
	}
}

func TestEliteAlignVisibleTargetAllowsBoundedUnknownHeatDuringEscapeCharge(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{
			visibleHeat("KNOWN", 54), visibleHeat("KNOWN", 55),
			visibleHeat("UNKNOWN", nil), visibleHeat("UNKNOWN", nil), visibleHeat("UNKNOWN", nil),
		},
		positions: []json.RawMessage{
			visiblePosition(250, 80, 262), visiblePosition(170, 80, 188), visiblePosition(100, 80, 128),
			visiblePosition(8, 5, 9.4), visiblePosition(7, 4, 8.1),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "ESCAPE VECTOR", "stopBeforeAlign": false, "positionSource": "ESCAPE_VECTOR", "heatPolicy": "ESCAPE_VECTOR_CHARGE",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if !contains(joinEventPhases(reporter.payloads), `HEAT_UNKNOWN_ESCAPE_CHARGE_GRACE:55`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetStrictPolicyStillFailsThreeUnknownHeatSamples(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{
			visibleHeat("KNOWN", 54), visibleHeat("UNKNOWN", nil), visibleHeat("UNKNOWN", nil), visibleHeat("UNKNOWN", nil),
		},
		positions: []json.RawMessage{
			visiblePosition(250, 80, 262), visiblePosition(170, 80, 188), visiblePosition(100, 80, 128),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "ESCAPE VECTOR", "stopBeforeAlign": false, "positionSource": "ESCAPE_VECTOR",
	}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "three consecutive samples") {
		t.Fatalf("error=%v", err)
	}
}

func TestEliteAlignVisibleTargetRejectsSingleFrameHighHeatOutlier(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{
			visibleHeat("KNOWN", 23), visibleHeat("KNOWN", 238), visibleHeat("KNOWN", 23), visibleHeat("KNOWN", 23),
		},
		positions: []json.RawMessage{
			visiblePosition(100, 20, 102), visiblePosition(8, 5, 9.4), visiblePosition(7, 4, 8.1),
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "ESCAPE VECTOR", "stopBeforeAlign": false, "positionSource": "ESCAPE_VECTOR",
	}, caller, reporter)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if !contains(joinEventPhases(reporter.payloads), `HIGH_HEAT_AWAITING_CONFIRMATION`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteAlignVisibleTargetFailsTwoConsecutiveHighHeatSamples(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 76), visibleHeat("KNOWN", 77)},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteAlignVisibleTargetPackage(t), map[string]any{
		"targetName": "LTT 11244 A 2", "stopBeforeAlign": false,
	}, caller, reporter)
	if err == nil || !contains(err.Error(), "confirmed 75 percent") {
		t.Fatalf("error=%v", err)
	}
	if !contains(joinEventPhases(reporter.payloads), `MAX_HEAT_PERCENT_CONFIRMED`) {
		t.Fatalf("events=%s", joinEventPhases(reporter.payloads))
	}
}
