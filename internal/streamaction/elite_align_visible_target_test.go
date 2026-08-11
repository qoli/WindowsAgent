package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type alignVisibleTargetCaller struct {
	heats     []json.RawMessage
	positions []json.RawMessage
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
	case "elite-dangerous/escape-vector-visible-position":
		if c.posIndex >= len(c.positions) {
			return nil, errors.New("unexpected position observation")
		}
		value := c.positions[c.posIndex]
		c.posIndex++
		return value, nil
	case "elite-dangerous/ship-attitude-control":
		control := inputs["control"].(string)
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
	value, _ := json.Marshal(map[string]any{"heat": map[string]any{"state": state, "percent": percent}})
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
