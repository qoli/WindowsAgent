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
	heats     []json.RawMessage
	positions []json.RawMessage
	controls  []string
	heatIndex int
	posIndex  int
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
	case "elite-dangerous/escape-vector-visible-position", "elite-dangerous/supercruise-target-position":
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

func visiblePosition(x, y, distance float64) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "target": map[string]any{
		"state": "DETECTED", "referenceX": 960 + x, "referenceY": 540 + y,
		"offsetX": x, "offsetY": y, "centerDistancePixels": distance,
		"reason": "TEST", "rawTexts": []string{"ESCAPE", "VECTOR"},
	}, "timing": map[string]any{}})
	return value
}

func unknownVisiblePosition() json.RawMessage {
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "target": map[string]any{
		"state": "UNKNOWN", "referenceX": nil, "referenceY": nil,
		"offsetX": nil, "offsetY": nil, "centerDistancePixels": nil,
		"reason": "TARGET_TEXT_NOT_FOUND", "rawTexts": []string{},
	}, "timing": map[string]any{}})
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

func TestEliteAlignVisibleTargetUsesRaisedMidFineDestinationPulse(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
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

func TestEliteAlignVisibleTargetUsesRaisedNearDestinationYawPulse(t *testing.T) {
	caller := &alignVisibleTargetCaller{
		heats: []json.RawMessage{visibleHeat("KNOWN", 23)},
		positions: []json.RawMessage{
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
