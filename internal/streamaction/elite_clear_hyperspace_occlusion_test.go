package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type clearHyperspaceOcclusionCaller struct {
	occlusions        []json.RawMessage
	occlusionIndex    int
	statusFlags       []int64
	statusIndex       int
	heatPercents      []int64
	heatIndex         int
	controls          []string
	throttles         []int64
	supercruiseToggle int
	slowObserverCalls int
	unsafeCharge      bool
	statusFailures    int
}

func (c *clearHyperspaceOcclusionCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/set-throttle":
		percent := inputs["percent"].(int64)
		c.throttles = append(c.throttles, percent)
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/hyperspace-target-occlusion":
		if c.occlusionIndex >= len(c.occlusions) {
			return nil, errors.New("unexpected obstruction observation")
		}
		result := c.occlusions[c.occlusionIndex]
		c.occlusionIndex++
		return result, nil
	case "elite-dangerous/ship-attitude-control":
		c.controls = append(c.controls, inputs["control"].(string))
		return json.RawMessage(`{"control":"PitchUpButton"}`), nil
	case "elite-dangerous/ship-status":
		return json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`), nil
	case "elite-dangerous/filesystem/status":
		if c.statusFailures > 0 {
			c.statusFailures--
			return nil, errors.New("transient Status launcher failure")
		}
		flags := int64(0)
		if c.supercruiseToggle == 1 {
			if c.unsafeCharge {
				flags = 131072 + 1048576
			} else {
				flags = 16
			}
		}
		if c.statusIndex < len(c.statusFlags) {
			flags = c.statusFlags[c.statusIndex]
		}
		c.statusIndex++
		return json.Marshal(map[string]any{
			"state": "AVAILABLE", "freshness": "CURRENT",
			"source": map[string]any{"sourceTimestamp": "status-" + string(rune('A'+c.statusIndex))},
			"data":   map[string]any{"Flags": flags, "Flags2": int64(0)},
		})
	case "elite-dangerous/ship-heat":
		percent := int64(54)
		if c.heatIndex < len(c.heatPercents) {
			percent = c.heatPercents[c.heatIndex]
		}
		c.heatIndex++
		return json.Marshal(map[string]any{"heat": map[string]any{"state": "KNOWN", "percent": percent}})
	case "elite-dangerous/ship-speed":
		// The fixed OCR ROI may become UNKNOWN under cockpit inertia while the
		// independent current-frame glyph topology still proves non-zero speed.
		return json.RawMessage(`{"speed":{"state":"UNKNOWN","displayValue":null},"evidence":{"zeroGlyph":{"state":"NOT_ZERO"}}}`), nil
	case "elite-dangerous/supercruise-control":
		c.supercruiseToggle++
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	case "elite-dangerous/supercruise-hud-state", "elite-dangerous/flight-prompt-text", "elite-dangerous/flight-status":
		c.slowObserverCalls++
		return nil, errors.New("slow visual observer must not be called after charge starts")
	default:
		return nil, errors.New("unexpected clear-occlusion child Action: " + id)
	}
}

func clearHyperspaceOcclusionObservation(state string, ratio, center float64, control any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"occlusion": map[string]any{
		"state": state, "stellarCoverageRatio": ratio, "centerCoverageRatio": center,
		"directionConfidence": 0.0, "recommendedControl": control,
	}})
	return value
}

func loadEliteClearHyperspaceOcclusionPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "clear-hyperspace-occlusion"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteClearHyperspaceOcclusionProbesThenEntersDedicatedSupercruise(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: []json.RawMessage{
			clearHyperspaceOcclusionObservation("BLOCKING", 1.0, 1.0, nil),
			clearHyperspaceOcclusionObservation("PARTIAL", 0.70, 0.70, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.08, 0.05, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("PARTIAL", 0.14, 0.00, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("PARTIAL", 0.15, 0.00, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("PARTIAL", 0.15, 0.00, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.07, 0.04, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.06, 0.04, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.05, 0.03, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.04, 0.03, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.04, 0.03, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.03, 0.02, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.03, 0.02, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.03, 0.02, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.02, 0.01, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.02, 0.01, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.02, 0.01, "PITCH_UP"),
		},
		heatPercents:   []int64{54, 53, 52},
		statusFailures: 1,
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"completed":true`, `"finalOcclusionState":"CLEAR"`, `"finalSupercruiseConfirmed":true`, `"supercruiseEntryPerformed":true`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.supercruiseToggle != 1 || !equalInt64s(caller.throttles, []int64{0, 100, 0, 100, 0}) {
		t.Fatalf("toggle=%d throttles=%v", caller.supercruiseToggle, caller.throttles)
	}
	if len(caller.controls) != 3 || caller.controls[0] != "PITCH_UP" {
		t.Fatalf("controls=%v", caller.controls)
	}
	if caller.slowObserverCalls != 0 {
		t.Fatalf("slow observer calls=%d", caller.slowObserverCalls)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"PROBING_DIRECTION", "STABILIZING_HEADING", "COOLING_BEFORE_CHARGE", "ENTERING_SUPERCRUISE", "SUPERCRUISE_ESCAPE", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing phase %s events=%s", phase, joined)
		}
	}
}

func TestEliteClearHyperspaceOcclusionCancelsUnsafeChargeWithoutSlowVisualPolling(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: []json.RawMessage{
			clearHyperspaceOcclusionObservation("BLOCKING", 1.0, 1.0, nil),
			clearHyperspaceOcclusionObservation("CLEAR", 0.08, 0.05, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.06, 0.04, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.05, 0.03, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.04, 0.03, "PITCH_UP"),
		},
		// Preflight, three cooling confirmations, unsafe charge observation,
		// then the first post-cancel confirmation.
		heatPercents: []int64{54, 53, 52},
		unsafeCharge: true,
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran", "normalSpaceSeparationConfirmed": true}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "cancelled after an unsafe Status flag") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseToggle != 2 {
		t.Fatalf("Supercruise toggles=%d, want start plus cancel", caller.supercruiseToggle)
	}
	if caller.slowObserverCalls != 0 {
		t.Fatalf("slow observer calls=%d", caller.slowObserverCalls)
	}
	if !equalInt64s(caller.throttles, []int64{0, 100, 0, 0}) {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	if !contains(joinEventPhases(reporter.payloads), `"phase":"RESUMING_AFTER_STELLAR_ESCAPE"`) {
		t.Fatalf("missing resume checkpoint event: %s", joinEventPhases(reporter.payloads))
	}
	if !contains(joinEventPhases(reporter.payloads), `"phase":"CANCELLING_CHARGE"`) {
		t.Fatalf("missing cancellation event: %s", joinEventPhases(reporter.payloads))
	}
}
