package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseLineOfSightDirectionCaller struct {
	target map[string]any
}

func (c *supercruiseLineOfSightDirectionCaller) Call(_ context.Context, id string, _ map[string]any) (json.RawMessage, error) {
	if id != "elite-dangerous/supercruise-target-position" {
		return nil, errors.New("unexpected line-of-sight direction child Action: " + id)
	}
	return json.Marshal(map[string]any{"target": c.target})
}

func loadEliteSupercruiseLineOfSightDirectionPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-line-of-sight-direction"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func lineOfSightDirectionTarget(offsetX, offsetY float64, presentation, plane string) map[string]any {
	return map[string]any{
		"state":                        "DETECTED",
		"referenceX":                   960 + offsetX,
		"referenceY":                   540 + offsetY,
		"offsetX":                      offsetX,
		"offsetY":                      offsetY,
		"centerDistancePixels":         180.0,
		"reason":                       "fixture",
		"presentation":                 presentation,
		"occupiedAngularBins":          int64(28),
		"angularRuns":                  int64(7),
		"reticleEvidencePlane":         plane,
		"reticleEvidenceQuality":       int64(900000),
		"reticleCapturedAt":            "2026-08-13T01:02:03Z",
		"shapeConfidencePermille":      int64(880),
		"layoutConfidencePermille":     int64(850),
		"textConfidencePermille":       int64(970),
		"focusFrameConfidencePermille": int64(900),
		"identityConfirmed":            true,
		"horizontalGapPixels":          30.0,
		"verticalGapPixels":            12.0,
		"rawTexts":                     []any{"OBAMA REACH"},
	}
}

func TestEliteSupercruiseLineOfSightDirectionSelectsDiagonalFromDashedHSVFrame(t *testing.T) {
	caller := &supercruiseLineOfSightDirectionCaller{target: lineOfSightDirectionTarget(140, -100, "DASHED", "HSV_ORANGE")}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteSupercruiseLineOfSightDirectionPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		&fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"READY"`) ||
		!contains(string(output), `"control":"PITCH_UP_YAW_RIGHT"`) ||
		!contains(string(output), `"initialProjectionPixels":100`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseLineOfSightDirectionRejectsNearCentreFrameWithoutDefault(t *testing.T) {
	target := lineOfSightDirectionTarget(20, 10, "DASHED", "HSV_ORANGE")
	target["centerDistancePixels"] = 22.36
	caller := &supercruiseLineOfSightDirectionCaller{target: target}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteSupercruiseLineOfSightDirectionPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		&fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) ||
		!contains(string(output), `"control":null`) ||
		!contains(string(output), `FOCUS_FRAME_TOO_CLOSE_TO_SCREEN_CENTER_FOR_BYPASS_DIRECTION`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseLineOfSightDirectionRejectsUnconfirmedCurrentIdentity(t *testing.T) {
	target := lineOfSightDirectionTarget(140, 0, "DASHED", "HSV_ORANGE")
	target["identityConfirmed"] = false
	caller := &supercruiseLineOfSightDirectionCaller{target: target}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(),
		loadEliteSupercruiseLineOfSightDirectionPackage(t),
		map[string]any{"targetName": "OBAMA REACH"},
		caller,
		&fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"state":"UNKNOWN"`) ||
		!contains(string(output), `CURRENT_TARGET_IDENTITY_NOT_CONFIRMED`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseLineOfSightDirectionRejectsNonHSVAndSolidEvidence(t *testing.T) {
	for _, test := range []struct {
		name         string
		presentation string
		plane        string
		wantReason   string
	}{
		{name: "non HSV plane", presentation: "DASHED", plane: "ORANGE_OPPONENT", wantReason: "HSV_ORANGE_FOCUS_FRAME_REQUIRED"},
		{name: "solid frame", presentation: "SOLID", plane: "HSV_ORANGE", wantReason: "DASHED_OCCLUDED_FOCUS_FRAME_REQUIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			caller := &supercruiseLineOfSightDirectionCaller{target: lineOfSightDirectionTarget(140, 0, test.presentation, test.plane)}
			output, err := (Runner{Sleep: immediateSleep}).Run(
				context.Background(),
				loadEliteSupercruiseLineOfSightDirectionPackage(t),
				map[string]any{"targetName": "OBAMA REACH"},
				caller,
				&fixtureReporter{},
			)
			if err != nil || !contains(string(output), `"state":"UNKNOWN"`) || !contains(string(output), test.wantReason) {
				t.Fatalf("output=%s error=%v", output, err)
			}
		})
	}
}
