package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseHUDCaller struct {
	dashboard json.RawMessage
	speed     json.RawMessage
}

func (c *supercruiseHUDCaller) Call(_ context.Context, id string, _ map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/request-docking-distance-regions":
		return c.dashboard, nil
	case "elite-dangerous/ship-speed-text-regions":
		return c.speed, nil
	default:
		return nil, errors.New("unexpected supercruise-hud-state child Action: " + id)
	}
}

func loadEliteSupercruiseHUDStatePackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-hud-state"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func ocrRegions(text string, detection, recognition float64) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"regions": []map[string]any{{"text": text, "detectionConfidence": detection, "recognitionConfidence": recognition}},
		"timing":  map[string]any{},
	})
	return value
}

func TestEliteSupercruiseHUDStateAcceptsSupercruiseSpeedUnitBeforeAssist(t *testing.T) {
	caller := &supercruiseHUDCaller{
		dashboard: ocrRegions("LTT 11244 A 2", 0.9, 0.9),
		speed:     ocrRegions("30.0km/s", 0.687, 0.974),
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteSupercruiseHUDStatePackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil || !contains(string(output), `"state":"ACTIVE"`) || !contains(string(output), `"supercruiseSpeedUnit":"KM/S"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseHUDStateRejectsPlainNormalSpaceSpeed(t *testing.T) {
	caller := &supercruiseHUDCaller{
		dashboard: ocrRegions("JAGDBADGER'S REST", 0.9, 0.9),
		speed:     ocrRegions("136", 0.9, 0.9),
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteSupercruiseHUDStatePackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil || !contains(string(output), `"state":"INACTIVE"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}
