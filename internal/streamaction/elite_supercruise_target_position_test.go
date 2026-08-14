package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseTargetPositionCaller struct {
	regions         map[string]json.RawMessage
	reticleShapeFor func(map[string]any) int
	reticleHints    []map[string]any
	childCalls      []string
}

func (c *supercruiseTargetPositionCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	c.childCalls = append(c.childCalls, id)
	if id == "elite-dangerous/supercruise-visible-reticle-position" {
		c.reticleHints = append(c.reticleHints, inputs)
		x := inputs["hintX"]
		y := inputs["hintY"]
		shapeConfidence := 900
		if c.reticleShapeFor != nil {
			shapeConfidence = c.reticleShapeFor(inputs)
		}
		value, _ := json.Marshal(map[string]any{"target": map[string]any{
			"state": "DETECTED", "referenceX": x, "referenceY": y,
			"offsetX": 0, "offsetY": 0, "centerDistancePixels": 0,
			"reason": "ORANGE_RETICLE_ANNULUS_CENTER_CONFIRMED", "bestScore": 30, "secondScore": 10,
			"presentation": "SOLID", "occupiedAngularBins": 60, "angularRuns": 1,
			"evidencePlane": "HSV_ORANGE", "evidenceQuality": 61000,
			"shapeConfidencePermille": shapeConfidence,
		}, "evidence": map[string]any{"capturedAt": "2026-08-13T01:02:03Z"}})
		return value, nil
	}
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

func targetPositionMultipleRegions(regions ...map[string]any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"regions": regions, "timing": map[string]any{}})
	return value
}

func targetPositionRegion(text string, left, centerY float64) map[string]any {
	return map[string]any{
		"text": text, "detectionConfidence": 0.96, "recognitionConfidence": 0.97,
		"referencePoints": []map[string]any{
			{"x": left, "y": centerY - 10}, {"x": left + 46, "y": centerY - 10},
			{"x": left + 46, "y": centerY + 10}, {"x": left, "y": centerY + 10},
		},
	}
}

func supercruiseTargetPositionBands(first, second, third json.RawMessage, rest ...json.RawMessage) map[string]json.RawMessage {
	upperLeft := targetPositionRegions("", 0, 0)
	fourth := targetPositionRegions("", 0, 0)
	identity := targetPositionRegions("", 0, 0)
	if len(rest) > 0 {
		fourth = rest[0]
	}
	if len(rest) > 1 {
		identity = rest[1]
	}
	return map[string]json.RawMessage{
		"elite-dangerous/supercruise-target-text-regions":              first,
		"elite-dangerous/supercruise-target-text-regions-lower":        second,
		"elite-dangerous/supercruise-target-text-regions-lower-wide":   third,
		"elite-dangerous/supercruise-target-text-regions-upper-left":   upperLeft,
		"elite-dangerous/supercruise-target-text-regions-upper-right":  fourth,
		"elite-dangerous/supercruise-target-text-regions-middle-left":  targetPositionRegions("", 0, 0),
		"elite-dangerous/supercruise-target-text-regions-middle-right": targetPositionRegions("", 0, 0),
		"elite-dangerous/request-docking-distance-regions":             identity,
	}
}

func TestEliteSupercruiseTargetPositionFindsMiddleLeftStation(t *testing.T) {
	regions := supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("HOUSSAY RING", 172, 780),
	)
	regions["elite-dangerous/supercruise-target-text-regions-middle-left"] = targetPositionRegions("HOUSSAY RING", 100, 474)
	caller := &supercruiseTargetPositionCaller{regions: regions}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "HOUSSAY RING"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"referenceX":70`) || !contains(string(output), `"referenceY":486`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionFindsIdentityCorroboratedMiddleRightSystemSuffix(t *testing.T) {
	regions := supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("AASGANANU", 172, 780),
	)
	regions["elite-dangerous/supercruise-target-text-regions-middle-right"] = targetPositionRegions("SGANANU", 1415, 420)
	caller := &supercruiseTargetPositionCaller{regions: regions}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "AASGANANU"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"OCCLUDED_SINGLE_WORD_POSITION_AND_EXACT_SELECTED_IDENTITY_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"referenceX":1385`) || !contains(string(output), `"referenceY":432`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionFindsIdentityCorroboratedOmittedNumericPrefix(t *testing.T) {
	regions := supercruiseTargetPositionBands(
		targetPositionRegions("ARIETIS", 1430, 350),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("47 ARIETIS", 172, 780),
	)
	caller := &supercruiseTargetPositionCaller{regions: regions}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "47 Arietis"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"OCCLUDED_SHORT_NUMERIC_PREFIX_AND_EXACT_SELECTED_IDENTITY_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"referenceX":1400`) || !contains(string(output), `"referenceY":362`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsUncorroboratedOmittedNumericPrefix(t *testing.T) {
	regions := supercruiseTargetPositionBands(
		targetPositionRegions("ARIETIS", 1430, 350),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
	)
	caller := &supercruiseTargetPositionCaller{regions: regions}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "47 Arietis"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) || !contains(string(output), `"reason":"TARGET_TEXT_NOT_FOUND"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionFindsUpperLeftTarget(t *testing.T) {
	regions := supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
	)
	regions["elite-dangerous/supercruise-target-text-regions-upper-left"] = targetPositionRegions("LP 298-42", 258, 390)
	caller := &supercruiseTargetPositionCaller{regions: regions}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LP 298-42"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"referenceX":228`) || !contains(string(output), `"referenceY":402`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionFindsUpperRightAssistTarget(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("SHAW STATION", 1565, 347),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"referenceX":1535`) ||
		!contains(string(output), `"referenceY":359`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionAcceptsPillarOccludedStationName(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("SW STATION", 1307.81, 329.415),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"OCCLUDED_SAME_LINE_PROPER_NAME_ENDPOINTS_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsWeakSameLineStationFragment(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("S STATION", 1307.81, 329.415),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionAcceptsIdentityCorroboratedPillarFragment(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("W STATION", 1400, 350),
		targetPositionRegions("SHAW STATION", 172, 780),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"OCCLUDED_POSITION_FRAGMENT_AND_EXACT_SELECTED_IDENTITY_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsUncorroboratedPillarFragment(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("W STATION", 1400, 350),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionCombinesPillarSplitSameLineWords(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("SHAW", 1280, 350),
			targetPositionRegion("TATION", 1380, 352),
		),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"PILLAR_SPLIT_SAME_LINE_WORDS_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"referenceX":1250`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionCombinesPillarSplitProperNamePrefix(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(targetPositionRegion("SHA", 1276, 329), targetPositionRegion("STATION", 1337, 326)),
		targetPositionRegions("SHAW STATION", 172, 780),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"EXACT_SELECTED_IDENTITY_AND_PROPER_NAME_PREFIX_SEARCH_HINT_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsVerticallySeparatedSplitWords(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("SHAW", 1280, 350),
			targetPositionRegion("TATION", 1380, 390),
		),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionAcceptsIdentityCorroboratedFusedPillarLabel(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("SHAViTATION", 1272.2, 329.78),
		targetPositionRegions("SHAW STATION", 172, 780),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"PILLAR_FUSED_POSITION_AND_EXACT_SELECTED_IDENTITY_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsUncorroboratedFusedPillarLabel(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("SHAViTATION", 1272.2, 329.78),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Shaw Station"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionSelectsForwardDuplicate(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 1030, 557.5),
		targetPositionRegions("LTT 11244 A 2", 230, 908),
		targetPositionRegions("", 0, 0),
	), reticleShapeFor: func(inputs map[string]any) int {
		if inputs["hintX"].(int64) > 500 {
			return 940
		}
		return 700
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"HIGHEST_SHAPE_LAYOUT_TEXT_CONFIDENCE_SELECTED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"centerDistancePixels":50`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionUsesMeasuredMarkerBelowLabel(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 988.73, 537.5),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"referenceX":958`) ||
		!contains(string(output), `"referenceY":550`) ||
		!contains(string(output), `"offsetY":10`) {
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
		!contains(string(output), `"reason":"TARGET_LABEL_TO_MARKER_OFFSET_APPLIED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) {
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
		!contains(string(output), `"reason":"FOCUS_FRAME_CANDIDATES_AMBIGUOUS"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsTextWhenShapeConfidenceIsLow(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("LTT 11244 A 2", 1030, 557.5),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
	), reticleShapeFor: func(map[string]any) int { return 519 }}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) ||
		!contains(string(output), `"reason":"NO_SHAPE_FIRST_FOCUS_FRAME_CANDIDATE"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionShapeScoresOnlyTextCompatibleProposals(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionMultipleRegions(
			targetPositionRegion("UNRELATED HUD TEXT", 600, 300),
			targetPositionRegion("LTT 11244 A 2", 1030, 557.5),
		),
		targetPositionRegions("", 0, 0),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "LTT 11244 A 2"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.reticleHints) != 1 {
		t.Fatalf("shape stage evaluated %d proposals, want only the target-compatible OCR proposal", len(caller.reticleHints))
	}
	if caller.reticleHints[0]["hintX"] != int64(1000) {
		t.Fatalf("reticle hint = %#v, want target-compatible label hint", caller.reticleHints[0])
	}
}

func TestEliteSupercruiseTargetPositionUsesBoundedLineOfSightScanProfile(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("UNRELATED CENTRAL TEXT", 600, 300),
		targetPositionRegions("OBAMA REACH", 990, 520),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteSupercruiseTargetPositionPackage(t),
		map[string]any{
			"targetName":             "OBAMA REACH",
			"scanProfile":            "LOS_DIRECTION",
			"reticleEvidencePolicy": "OCCLUSION_AWARE",
		},
		caller,
		&fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	for _, id := range caller.childCalls {
		if id == "elite-dangerous/supercruise-target-text-regions" ||
			id == "elite-dangerous/supercruise-target-text-regions-upper-left" ||
			id == "elite-dangerous/supercruise-target-text-regions-upper-right" ||
			id == "elite-dangerous/supercruise-target-text-regions-middle-left" ||
			id == "elite-dangerous/supercruise-target-text-regions-middle-right" {
			t.Fatalf("LOS profile invoked excluded OCR band %q", id)
		}
	}
	if len(caller.reticleHints) != 1 || caller.reticleHints[0]["evidencePolicy"] != "OCCLUSION_AWARE" {
		t.Fatalf("reticle inputs = %#v", caller.reticleHints)
	}
}

func TestEliteSupercruiseTargetPositionCombinesOccludedTwoLineStationName(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("CREL", 1307, 660),
			targetPositionRegion("STAN", 1307, 680),
		),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "CREON'S STANDING"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"OCCLUDED_TWO_LINE_WORD_PREFIXES_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"referenceX":1277`) ||
		!contains(string(output), `"referenceY":682`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionCombinesStackedMultiwordSystemName(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("TASCHETER", 1133.57, 417.36),
			targetPositionRegion("SECTOR TE-Q A5-1", 1132.92, 436.31),
		),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Tascheter Sector TE-Q a5-1"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"DETECTED"`) ||
		!contains(string(output), `"reason":"STACKED_FULL_TARGET_NAME_CONFIRMED:CURRENT_FRAME_RETICLE_CONFIRMED:HSV_ORANGE:SOLID"`) ||
		!contains(string(output), `"referenceX":1103`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsStackedPartialSystemName(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("TASCHETER", 1133.57, 417.36),
			targetPositionRegion("SECTOR TE-Q", 1132.92, 436.31),
		),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "Tascheter Sector TE-Q a5-1"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsUnrelatedTwoLineFragments(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("CRAD", 1307, 660),
			targetPositionRegion("STOP", 1307, 680),
		),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "CREON'S STANDING"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) ||
		!contains(string(output), `"reason":"TARGET_TEXT_NOT_FOUND"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseTargetPositionRejectsSeparatedWordPrefixes(t *testing.T) {
	caller := &supercruiseTargetPositionCaller{regions: supercruiseTargetPositionBands(
		targetPositionRegions("", 0, 0),
		targetPositionMultipleRegions(
			targetPositionRegion("CREL", 1307, 660),
			targetPositionRegion("STAN", 1360, 720),
		),
		targetPositionRegions("", 0, 0),
	)}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseTargetPositionPackage(t), map[string]any{"targetName": "CREON'S STANDING"}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}
