package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type clearHyperspaceOcclusionCaller struct {
	occlusions             []json.RawMessage
	occlusionIndex         int
	compassTargets         []json.RawMessage
	compassIndex           int
	compassCallIndex       int
	compassUnavailableAt   map[int]bool
	heatPercents           []int64
	heatIndex              int
	heatUnknownAt          map[int]bool
	controls               []string
	holdOperations         []string
	holdControls           []string
	throttles              []int64
	supercruiseToggle      int
	statusIndex            int
	lastStatusTime         string
	staleCancelReads       int
	unsafeCharge           bool
	statusFailures         int
	statusFreshness        string
	preventEntry           bool
	visibleAligned         bool
	atFullThrottle         bool
	enteredSupercruise     bool
	entryCountdownReads    int
	countdownReadIndex     int
	countdownOverheat      bool
	visiblePositionUnknown bool
}

func (c *clearHyperspaceOcclusionCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/set-throttle":
		percent := inputs["percent"].(int64)
		c.throttles = append(c.throttles, percent)
		c.atFullThrottle = percent == 100
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/hyperspace-target-occlusion":
		if c.occlusionIndex >= len(c.occlusions) {
			return nil, errors.New("unexpected obstruction observation")
		}
		result := c.occlusions[c.occlusionIndex]
		c.occlusionIndex++
		return result, nil
	case "elite-dangerous/compass":
		if c.supercruiseToggle%2 == 0 {
			return escapeVectorTarget("SOLID", 100, 0, 100), nil
		}
		callIndex := c.compassCallIndex
		c.compassCallIndex++
		if c.compassUnavailableAt[callIndex] {
			return nil, errors.New("COMPASS_NOT_VISIBLE")
		}
		if c.compassIndex >= len(c.compassTargets) {
			return nil, errors.New("escape vector missing")
		}
		result := c.compassTargets[c.compassIndex]
		c.compassIndex++
		return result, nil
	case "elite-dangerous/escape-vector-visible-position":
		if c.visiblePositionUnknown {
			return json.RawMessage(`{"schemaVersion":1,"target":{"state":"UNKNOWN","referenceX":null,"referenceY":null,"offsetX":null,"offsetY":null,"centerDistancePixels":null,"reason":"TEST_OUTSIDE_ROI","rawTexts":[]},"timing":{}}`), nil
		}
		return json.RawMessage(`{"schemaVersion":1,"target":{"state":"DETECTED","referenceX":960,"referenceY":540,"offsetX":0,"offsetY":0,"centerDistancePixels":0,"reason":"TEST","rawTexts":["ESCAPE","VECTOR"]},"timing":{}}`), nil
	case "elite-dangerous/align-visible-target":
		c.visibleAligned = true
		return json.RawMessage(`{"schemaVersion":1,"task":"ALIGN_VISIBLE_TARGET","completed":true,"targetName":"ESCAPE VECTOR","sampleCount":3,"commandCount":0,"stableConfirmations":3,"finalTarget":{"state":"DETECTED"}}`), nil
	case "elite-dangerous/ship-attitude-control":
		if inputs["holdMs"].(int64) > 1000 {
			return nil, errors.New("attitude pulse exceeds the finite Action contract")
		}
		c.controls = append(c.controls, inputs["control"].(string))
		return json.RawMessage(`{"control":"attitude"}`), nil
	case "elite-dangerous/ship-attitude-hold":
		operation := inputs["operation"].(string)
		c.holdOperations = append(c.holdOperations, operation)
		c.holdControls = append(c.holdControls, inputs["control"].(string))
		return json.RawMessage(`{"leaseId":"key_0123456789abcdef0123456789abcdef","state":"ACTIVE"}`), nil
	case "elite-dangerous/ship-status":
		return json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`), nil
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"schemaVersion":1,"text":"ALIGN WITH ESCAPE VECTOR","confidence":0.99,"evidence":{},"model":{},"timing":{}}`), nil
	case "elite-dangerous/flight-status":
		return json.RawMessage(`{"flightStatus":{"state":"FSD_ESCAPE_VECTOR_REQUIRED","known":true}}`), nil
	case "elite-dangerous/filesystem/status":
		if c.statusFailures > 0 {
			c.statusFailures--
			return nil, errors.New("transient Status launcher failure")
		}
		flags := int64(0)
		if c.enteredSupercruise {
			flags = 16
		} else if c.supercruiseToggle%2 == 1 {
			if c.unsafeCharge {
				flags = 131072 + 1048576
			} else if c.visibleAligned && c.atFullThrottle && c.countdownReadIndex < c.entryCountdownReads {
				flags = 131072
				if c.countdownOverheat {
					flags += 1048576
				}
				c.countdownReadIndex++
			} else if !c.preventEntry && ((c.visibleAligned && c.atFullThrottle) || (c.supercruiseToggle >= 3 && len(c.compassTargets) > 0 && c.compassIndex >= len(c.compassTargets))) {
				flags = 16
				c.enteredSupercruise = true
			} else {
				flags = 131072
			}
		}
		timestamp := c.lastStatusTime
		if c.supercruiseToggle%2 == 0 && c.staleCancelReads > 0 {
			c.staleCancelReads--
		} else {
			c.statusIndex++
			timestamp = "status-" + string(rune('A'+c.statusIndex))
			c.lastStatusTime = timestamp
		}
		freshness := c.statusFreshness
		if freshness == "" {
			freshness = "CURRENT"
		}
		return json.Marshal(map[string]any{
			"state": "AVAILABLE", "freshness": freshness,
			"source": map[string]any{"sourceTimestamp": timestamp},
			"data":   map[string]any{"Flags": flags, "Flags2": int64(0)},
		})
	case "elite-dangerous/ship-heat":
		if c.heatUnknownAt[c.heatIndex] {
			c.heatIndex++
			return json.RawMessage(`{"heat":{"state":"UNKNOWN","percent":null}}`), nil
		}
		percent := int64(54)
		if c.heatIndex < len(c.heatPercents) {
			percent = c.heatPercents[c.heatIndex]
		}
		c.heatIndex++
		return json.Marshal(map[string]any{"heat": map[string]any{"state": "KNOWN", "percent": percent}})
	case "elite-dangerous/supercruise-control":
		c.supercruiseToggle++
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	default:
		return nil, errors.New("unexpected clear-occlusion child Action: " + id)
	}
}

func clearHyperspaceOcclusionObservation(state string, ratio, center float64, control any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"occlusion": map[string]any{
		"state": state, "stellarCoverageRatio": ratio, "centerCoverageRatio": center,
		"maximumCellCoverageRatio": ratio, "directionConfidence": 0.5, "recommendedControl": control, "safeToCharge": ratio <= 0.005,
	}})
	return value
}

func escapeVectorTarget(presentation string, x, y int64, distance float64) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"target": map[string]any{
		"detected": true, "presentation": presentation, "offsetX": x, "offsetY": y,
		"centerDistancePixels": distance, "centerZone": map[string]any{"inside": distance <= 4},
	}})
	return value
}

func repeatCompassTargets(targets ...json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, 0, len(targets)*8)
	for _, target := range targets {
		for range 2 {
			result = append(result, target)
		}
	}
	return result
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

func clearThenCruiseOcclusions() []json.RawMessage {
	values := []json.RawMessage{
		clearHyperspaceOcclusionObservation("BLOCKING", 0.75, 1.0, "PITCH_DOWN"),
		clearHyperspaceOcclusionObservation("CLEAR", 0.08, 0.05, "PITCH_DOWN"),
		clearHyperspaceOcclusionObservation("CLEAR", 0.06, 0.04, "PITCH_DOWN"),
		clearHyperspaceOcclusionObservation("CLEAR", 0.04, 0.03, "PITCH_DOWN"),
	}
	for range 14 {
		values = append(values, clearHyperspaceOcclusionObservation("CLEAR", 0.002, 0, "PITCH_DOWN"))
	}
	return values
}

func TestEliteClearHyperspaceOcclusionAlignsEscapeVectorThenEntersSupercruise(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: clearThenCruiseOcclusions(),
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("HOLLOW", -30, 20, 36),
			escapeVectorTarget("SOLID", 18, 9, 20),
			escapeVectorTarget("SOLID", 1, -1, 1.4),
			escapeVectorTarget("SOLID", 1, -1, 1.4),
		),
		heatPercents:   []int64{54, 53, 52, 53, 54, 55},
		heatUnknownAt:  map[int]bool{3: true},
		statusFailures: 1,
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("%v compassIndex=%d events=%s", err, caller.compassIndex, joinEventPhases(reporter.payloads))
	}
	if caller.heatIndex != 8 {
		t.Fatalf("ship-heat calls=%d, want initial and inter-probe heat confirmations", caller.heatIndex)
	}
	for _, expected := range []string{`"completed":true`, `"escapeVectorDetected":true`, `"escapeVectorAlignmentConfirmations":3`, `"finalSupercruiseConfirmed":true`, `"restoreHyperspaceDestinationRequired":true`} {
		if !contains(string(output), expected) {
			t.Fatalf("missing %s output=%s", expected, output)
		}
	}
	if caller.supercruiseToggle != 3 || !equalInt64s(caller.throttles, []int64{0, 0, 100, 0}) {
		t.Fatalf("toggle=%d throttles=%v", caller.supercruiseToggle, caller.throttles)
	}
	if len(caller.holdControls) < 2 || caller.holdControls[0] != "PITCH_UP" || caller.holdControls[1] != "PITCH_UP" {
		t.Fatalf("prealignment hold controls=%v", caller.holdControls)
	}
	if len(caller.holdOperations) < 2 || caller.holdOperations[0] != "START" || caller.holdOperations[1] != "STOP" {
		t.Fatalf("prealignment segment must own a bounded hold lease, operations=%v", caller.holdOperations)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"OBSERVING_FORWARD_VIEW", "COOLING_BEFORE_CHARGE", "PROBING_ESCAPE_VECTOR", "PREALIGNING_ESCAPE_VECTOR", "OBSERVING_POST_PULSE_VIEW", "SUPERCRUISE_ESCAPE", "COMPLETED"} {
		if !contains(joined, `"phase":"`+phase+`"`) {
			t.Fatalf("missing phase %s events=%s", phase, joined)
		}
	}
	for _, evidenceState := range []string{"LIVE_CHARGE", "CACHED_ONE_SHOT", "EXPIRED"} {
		if !contains(joined, `"escapeVectorEvidenceState":"`+evidenceState+`"`) {
			t.Fatalf("missing ephemeral Compass evidence state %s events=%s", evidenceState, joined)
		}
	}
	if !contains(joined, `"phase":"PREALIGNING_ESCAPE_VECTOR"`) || !contains(joined, `"reason":"TURN_SEGMENT_FROM_CACHED_ONE_SHOT_SNAPSHOT"`) {
		t.Fatalf("prealignment pulse must be authorized by one cached snapshot: %s", joined)
	}
	if !contains(joined, `"phase":"ALIGNING_VISIBLE_ESCAPE_VECTOR"`) || !contains(joined, `"reason":"VISIBLE_ESCAPE_VECTOR_ALIGNMENT_COMPLETED"`) {
		t.Fatalf("SOLID plus a detected ROI must hand off to visible-target alignment: %s", joined)
	}
}

func TestEliteClearHyperspaceOcclusionUsesFixedCoarseDirectionForHollowProbe(t *testing.T) {
	occlusions := []json.RawMessage{}
	for range 10 {
		occlusions = append(occlusions, clearHyperspaceOcclusionObservation("CLEAR", 0.002, 0, "PITCH_DOWN"))
	}
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: occlusions,
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("HOLLOW", -2, -5, 5.4),
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("SOLID", 0, 0, 0),
		),
		heatPercents:  []int64{54, 53, 52, 54, 53, 52, 51, 52},
		heatUnknownAt: map[int]bool{},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("%v controls=%v events=%s", err, caller.controls, joinEventPhases(reporter.payloads))
	}
	if len(caller.holdControls) < 1 || caller.holdControls[0] != "PITCH_UP" {
		t.Fatalf("hold controls=%v, want fixed coarse PITCH_UP for rear projection", caller.holdControls)
	}
	if contains(joinEventPhases(reporter.payloads), `"reason":"UNSAFE_TRIAL_ROLLED_BACK"`) {
		t.Fatalf("CV must not override Escape Vector direction: %s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteClearHyperspaceOcclusionCancelsUnsafeCharge(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions:       clearThenCruiseOcclusions()[:8],
		heatPercents:     []int64{54, 53, 52},
		unsafeCharge:     true,
		staleCancelReads: 2,
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "prealignment probe crossed a Status safety gate") {
		t.Fatalf("error=%v", err)
	}
	if caller.heatIndex != 3 {
		t.Fatalf("ship-heat calls=%d, want no synchronous OCR after charge starts", caller.heatIndex)
	}
	if caller.supercruiseToggle != 2 {
		t.Fatalf("Supercruise toggles=%d, want start plus cancel", caller.supercruiseToggle)
	}
	if !contains(joinEventPhases(reporter.payloads), `"phase":"CANCELLING_CHARGE"`) {
		t.Fatalf("missing cancellation event: %s", joinEventPhases(reporter.payloads))
	}
	if !contains(joinEventPhases(reporter.payloads), `"reason":"WAITING_POST_CANCEL_STATUS"`) {
		t.Fatalf("stale post-cancel Status was not rejected: %s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteClearHyperspaceOcclusionCancelsAtLocalVisualHeatGate(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: clearThenCruiseOcclusions()[:8],
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("HOLLOW", -30, 20, 36),
			escapeVectorTarget("SOLID", 18, 9, 20),
			escapeVectorTarget("SOLID", 1, -1, 1.4),
		),
		heatPercents: []int64{54, 53, 52, 54, 55, 56, 160},
		preventEntry: true,
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "cancelled by the local visual heat gate") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseToggle != 2 || caller.heatIndex != 7 {
		t.Fatalf("toggles=%d heatCalls=%d", caller.supercruiseToggle, caller.heatIndex)
	}
	if !contains(joinEventPhases(reporter.payloads), `"reason":"CHARGE_HEAT_GATE"`) {
		t.Fatalf("missing heat cancellation event: %s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteClearHyperspaceOcclusionAllowsOverheatFlagBelowCountdownHeatLimit(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: clearThenCruiseOcclusions(),
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
		),
		heatPercents:        []int64{54, 53, 52, 54, 159, 159},
		entryCountdownReads: 2,
		countdownOverheat:   true,
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err != nil {
		t.Fatalf("countdown heat below 160 must remain allowed: %v events=%s", err, joinEventPhases(reporter.payloads))
	}
	if !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"CHECKING_COUNTDOWN_HEAT"`) || !contains(joined, `"heatPercent":159`) || !contains(joined, `"overHeating":true`) {
		t.Fatalf("missing tolerant countdown heat evidence: %s", joined)
	}
}

func TestEliteClearHyperspaceOcclusionAcceptsPersistentStatusBaseline(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: clearThenCruiseOcclusions(),
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("SOLID", 0, 0, 0),
		),
		heatPercents:    []int64{54, 53, 52, 53},
		statusFreshness: "STALE",
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEliteClearHyperspaceOcclusionCancelsAfterThreeUnknownChargeHeatSamples(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: clearThenCruiseOcclusions()[:8],
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("SOLID", 18, 9, 20),
			escapeVectorTarget("SOLID", 12, 6, 13.4),
			escapeVectorTarget("SOLID", 8, 4, 8.9),
			escapeVectorTarget("SOLID", 7, 4, 8.1),
			escapeVectorTarget("SOLID", 6, 3, 6.7),
		),
		heatPercents:           []int64{54, 53, 52},
		heatUnknownAt:          map[int]bool{6: true, 7: true, 8: true},
		preventEntry:           true,
		visiblePositionUnknown: true,
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "cancelled by the local visual heat gate") {
		t.Fatalf("error=%v", err)
	}
	if caller.heatIndex != 9 || caller.supercruiseToggle != 4 {
		t.Fatalf("heatCalls=%d toggles=%d", caller.heatIndex, caller.supercruiseToggle)
	}
}

func TestEliteClearHyperspaceOcclusionFailsClosedWhenEscapeVectorNeverAppears(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions:   clearThenCruiseOcclusions()[:8],
		heatPercents: []int64{54, 53, 52},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err == nil || !contains(err.Error(), "could not confirm Compass ownership") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseToggle != 2 {
		t.Fatalf("Supercruise toggles=%d, want explicit cancellation", caller.supercruiseToggle)
	}
}

func TestEliteClearHyperspaceOcclusionToleratesFlashingCompassSamples(t *testing.T) {
	occlusions := []json.RawMessage{}
	for range 8 {
		occlusions = append(occlusions, clearHyperspaceOcclusionObservation("CLEAR", 0.002, 0, "PITCH_DOWN"))
	}
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: occlusions,
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("SOLID", 0, 0, 0),
		),
		compassUnavailableAt: map[int]bool{0: true, 2: true, 4: true, 6: true},
		heatPercents:         []int64{54, 53, 52, 53},
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(joinEventPhases(reporter.payloads), `COMPASS_SAMPLE_UNAVAILABLE`) {
		t.Fatalf("missing flashing Compass evidence: %s", joinEventPhases(reporter.payloads))
	}
}

func TestEliteClearHyperspaceOcclusionAcceptsStableEdgeOnlyPartialInitialHeading(t *testing.T) {
	caller := &clearHyperspaceOcclusionCaller{
		occlusions: []json.RawMessage{
			clearHyperspaceOcclusionObservation("BLOCKING", 0.75, 1.0, "PITCH_DOWN"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.004, 0, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.003, 0, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.002, 0, "PITCH_UP"),
			clearHyperspaceOcclusionObservation("CLEAR", 0.001, 0, "PITCH_UP"),
		},
		compassTargets: repeatCompassTargets(
			escapeVectorTarget("SOLID", 0, 0, 0),
			escapeVectorTarget("SOLID", 0, 0, 0),
		),
		heatPercents: []int64{54, 53, 52},
	}
	// The cruise tail needs six periodic and one final CLEAR observations.
	for range 7 {
		caller.occlusions = append(caller.occlusions, clearHyperspaceOcclusionObservation("CLEAR", 0.01, 0, "PITCH_UP"))
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteClearHyperspaceOcclusionPackage(t), map[string]any{"targetName": "Aldebaran"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("forward-view CV must remain diagnostic-only, controls=%v", caller.controls)
	}
}
