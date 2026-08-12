package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseTargetPositionCaller struct {
	regions map[string]json.RawMessage
}

func (c *supercruiseTargetPositionCaller) Call(_ context.Context, id string, _ map[string]any) (json.RawMessage, error) {
	value, ok := c.regions[id]
	if !ok {
		return nil, errors.New("unexpected supercruise-target-position child Action: " + id)
	}
	return value, nil
}

func loadEliteSupercruiseTargetPositionPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-target-position"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func targetPositionRegions(text string, left, centerY float64) json.RawMessage {
	regions := []map[string]any{}
	if text != "" {
		regions = append(regions, map[string]any{
			"text": text, "detectionConfidence": 0.96, "recognitionConfidence": 0.97,
			"referencePoints": []map[string]any{
				{"x": left, "y": centerY - 10}, {"x": left + 180, "y": centerY - 10},
				{"x": left + 180, "y": centerY + 10}, {"x": left, "y": centerY + 10},
			},
		})
	}
	value, _ := json.Marshal(map[string]any{"regions": regions, "timing": map[string]any{}})
	return value
}

func supercruiseTargetPositionBands(first, second, third json.RawMessage) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"elite-dangerous/supercruise-target-text-regions":            first,
		"elite-dangerous/supercruise-target-text-regions-lower":      second,
		"elite-dangerous/supercruise-target-text-regions-lower-wide": third,
	}
}

func TestEliteSupercruiseTargetPositionSelectsForwardDuplicate(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 1030, 578),
		targetPositionRegions("LTT 11244 A 2", 230, 908),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"NEAREST_FORWARD_TARGET_LABEL_SELECTED"`) ||
		!contains(string(output), `"centerDistancePixels":50`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionDeduplicatesOverlappingBandBoundary(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 1030, 578),
		targetPositionRegions("LTT 11244 A 2", 1039, 579),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"TARGET_LABEL_TO_MARKER_OFFSET_APPLIED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsNearEqualDuplicates(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 1030, 578),
		targetPositionRegions("LTT 11244 A 2", 1050, 578),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) ||
		!contains(string(output), `"reason":"TARGET_TEXT_CANDIDATES_AMBIGUOUS"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}
