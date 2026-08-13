package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/capture"
)

type hyperspaceJumpCaller struct {
	hyperspaceObservations int
	hyperspaceFailuresAt   map[int][]error
	hyperspaceControls     int
	alignVisibleError      error
	throttles              []int
	forceObstruction       bool
	postAlignObstruction   bool
	heatUnknown            bool
	occlusionCalls         int
	clearStartModes        []string
	alignmentPurposes      []string
	alignmentProfiles      []string
	alignmentRequired      bool
	journalCaseVariant     bool
	calls                  []string
}

func (c *hyperspaceJumpCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/align-station-target":
		c.calls = append(c.calls, id)
		c.alignmentPurposes = append(c.alignmentPurposes, inputs["alignmentPurpose"].(string))
		c.alignmentProfiles = append(c.alignmentProfiles, inputs["controlProfile"].(string))
		return json.RawMessage(`{"completed":true,"sampleCount":3,"stableConfirmations":3,"finalObservation":{"target":{"detected":true,"presentation":"SOLID","centerDistancePixels":3}}}`), nil
	case "elite-dangerous/hyperspace-target-occlusion":
		c.calls = append(c.calls, id)
		c.occlusionCalls++
		if c.forceObstruction && c.occlusionCalls == 1 {
			return json.RawMessage(`{"occlusion":{"state":"BLOCKING","stellarCoverageRatio":0.2,"centerCoverageRatio":0.01,"maximumCellCoverageRatio":1,"directionConfidence":0.9,"recommendedControl":"PITCH_UP","safeToCharge":false}}`), nil
		}
		if c.postAlignObstruction && c.occlusionCalls == 2 {
			return json.RawMessage(`{"occlusion":{"state":"BLOCKING","stellarCoverageRatio":0.3,"centerCoverageRatio":0.2,"maximumCellCoverageRatio":0.8,"directionConfidence":0.9,"recommendedControl":"YAW_LEFT","safeToCharge":false}}`), nil
		}
		return json.RawMessage(`{"occlusion":{"state":"CLEAR","stellarCoverageRatio":0.002,"centerCoverageRatio":0,"maximumCellCoverageRatio":0.01,"directionConfidence":0.8,"recommendedControl":"PITCH_UP","safeToCharge":true}}`), nil
	case "elite-dangerous/clear-hyperspace-occlusion":
		c.calls = append(c.calls, id)
		c.clearStartModes = append(c.clearStartModes, inputs["startMode"].(string))
		return json.RawMessage(`{"completed":true,"finalOcclusionState":"CLEAR"}`), nil
	case "elite-dangerous/align-visible-target":
		c.calls = append(c.calls, id)
		if c.alignVisibleError != nil {
			return nil, c.alignVisibleError
		}
		return json.RawMessage(`{"completed":true,"sampleCount":3,"finalTarget":{"presentation":"SOLID"}}`), nil
	case "elite-dangerous/ship-heat":
		c.calls = append(c.calls, id)
		if c.heatUnknown {
			return json.RawMessage(`{"heat":{"state":"UNKNOWN","percent":null}}`), nil
		}
		return json.RawMessage(`{"heat":{"state":"KNOWN","percent":46}}`), nil
	case "elite-dangerous/hyperspace-state":
		if failures := c.hyperspaceFailuresAt[c.hyperspaceObservations]; len(failures) > 0 {
			err := failures[0]
			c.hyperspaceFailuresAt[c.hyperspaceObservations] = failures[1:]
			return nil, err
		}
		c.hyperspaceObservations++
		state := "COCKPIT_PRESENT"
		cockpit := "PRESENT"
		if c.hyperspaceControls > 0 {
			if c.journalCaseVariant {
				if c.hyperspaceObservations == 2 {
					state = "FSD_CHARGING"
				}
			} else {
				switch c.hyperspaceObservations {
				case 2:
					if c.alignmentRequired {
						state = "ALIGNMENT_REQUIRED"
					} else {
						state = "FSD_CHARGING"
					}
				case 3, 4:
					state = "COCKPIT_ABSENT"
					cockpit = "ABSENT"
				default:
					state = "COCKPIT_PRESENT"
				}
			}
		}
		return json.Marshal(map[string]any{"hyperspaceState": map[string]any{
			"state": state, "flightStatus": "UNKNOWN", "cockpitHud": map[string]any{"state": cockpit}, "promptText": "",
		}})
	case "elite-dangerous/filesystem/journal-navigation-tail":
		if c.journalCaseVariant && c.hyperspaceControls > 0 {
			return json.RawMessage(`{"events":[{"event":"FSDJump","timestamp":"2026-08-13T05:43:10Z","StarSystem":"Aasgananu","SystemAddress":9466510452105}]}`), nil
		}
		return json.RawMessage(`{"events":[]}`), nil
	case "elite-dangerous/hyperspace-control":
		c.hyperspaceControls++
		return json.RawMessage(`{"control":"HyperSuperCombination"}`), nil
	case "elite-dangerous/set-throttle":
		percent := int(inputs["percent"].(int64))
		c.throttles = append(c.throttles, percent)
		return json.Marshal(map[string]any{"control": "SetSpeed"})
	case "elite-dangerous/supercruise-hud-state":
		return json.RawMessage(`{"supercruiseHud":{"state":"ACTIVE"}}`), nil
	default:
		return nil, errors.New("unexpected hyperspace child Action: " + id)
	}
}

func TestEliteHyperspaceJumpReclearsBeforeVisibleAlignmentWhenCompassPointsBackIntoStar(t *testing.T) {
	caller := &hyperspaceJumpCaller{postAlignObstruction: true}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.clearStartModes) != 1 || caller.clearStartModes[0] != "SUPERCRUISE" {
		t.Fatalf("post-alignment clear start modes=%v", caller.clearStartModes)
	}
	alignCalls := 0
	for _, call := range caller.calls {
		if call == "elite-dangerous/align-station-target" {
			alignCalls++
		}
	}
	if alignCalls != 2 || caller.hyperspaceControls != 1 {
		t.Fatalf("alignCalls=%d hyperspaceControls=%d calls=%v", alignCalls, caller.hyperspaceControls, caller.calls)
	}
	firstVisible := -1
	firstClear := -1
	for index, call := range caller.calls {
		if call == "elite-dangerous/clear-hyperspace-occlusion" && firstClear < 0 {
			firstClear = index
		}
		if call == "elite-dangerous/align-visible-target" && firstVisible < 0 {
			firstVisible = index
		}
	}
	if firstClear < 0 || firstVisible < 0 || firstClear > firstVisible {
		t.Fatalf("stellar clearance must precede visible-target refinement, calls=%v", caller.calls)
	}
}

func TestEliteHyperspaceJumpFailsClosedOnUnknownPostAlignmentHeat(t *testing.T) {
	caller := &hyperspaceJumpCaller{heatUnknown: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err == nil || !strings.Contains(err.Error(), "post-alignment ship heat") {
		t.Fatalf("error=%v", err)
	}
	if caller.hyperspaceControls != 0 {
		t.Fatalf("unknown heat must block all FSD input, controls=%d", caller.hyperspaceControls)
	}
}

func loadEliteHyperspaceJumpPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "hyperspace-jump-to-system"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func hyperspaceJumpInputs() map[string]any {
	return map[string]any{
		"targetSystem": "87 Mu Ceti", "targetLockConfirmed": true,
		"startMode": "NORMAL_SPACE", "normalSpaceConfirmed": true, "supercruiseConfirmed": false,
	}
}

func TestEliteHyperspaceJumpRejectsVisibleTargetDomainUnknownBeforeFSD(t *testing.T) {
	caller := &hyperspaceJumpCaller{alignVisibleError: errors.New("fail: visible target remained UNKNOWN after its bounded observation window")}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err == nil || !strings.Contains(err.Error(), "visible target remained UNKNOWN") {
		t.Fatalf("error=%v", err)
	}
	if caller.hyperspaceControls != 0 || len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("controls=%d throttles=%v", caller.hyperspaceControls, caller.throttles)
	}
}

func TestEliteHyperspaceJumpNormalSpaceUsesNormalSpaceCompassProfile(t *testing.T) {
	caller := &hyperspaceJumpCaller{alignVisibleError: errors.New("stop after profile evidence")}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err == nil || len(caller.alignmentProfiles) != 1 || caller.alignmentProfiles[0] != "NORMAL_SPACE" {
		t.Fatalf("error=%v alignmentProfiles=%v", err, caller.alignmentProfiles)
	}
}

func TestEliteHyperspaceJumpAcceptsCanonicalJournalCaseForUppercaseHUDTarget(t *testing.T) {
	caller := &hyperspaceJumpCaller{journalCaseVariant: true}
	inputs := hyperspaceJumpInputs()
	inputs["targetSystem"] = "AASGANANU"
	inputs["targetSystemAddress"] = int64(9466510452105)
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), inputs, caller, &fixtureReporter{})
	if err != nil || !strings.Contains(string(output), `"completed":true`) || !strings.Contains(string(output), `"arrivalEvidence":"JOURNAL_FSDJUMP"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.throttles) < 2 || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("Journal arrival must send the arrival brake, throttles=%v", caller.throttles)
	}
}

func TestEliteHyperspaceJumpSkipsBoundedWGCFailureDuringCountdownTransition(t *testing.T) {
	caller := &hyperspaceJumpCaller{hyperspaceFailuresAt: map[int][]error{
		2: {errors.New("child Action elite-dangerous/flight-prompt-text failed: capture OCR Action region: persistent WGC worker region capture: persistent region capture failed: mapped region texture has an invalid row pitch")},
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err != nil || !strings.Contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if len(caller.throttles) < 2 || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("transient WGC recovery must still complete with arrival brake: throttles=%v", caller.throttles)
	}
}

func TestEliteHyperspaceJumpCancelsAndFailsInsteadOfRealigningDuringCharge(t *testing.T) {
	caller := &hyperspaceJumpCaller{alignmentRequired: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err == nil || !strings.Contains(err.Error(), "ALIGNMENT_REQUIRED") || !strings.Contains(err.Error(), "charge cancelled") {
		t.Fatalf("error=%v", err)
	}
	if caller.hyperspaceControls != 2 {
		t.Fatalf("expected one start and one cancel FSD control, controls=%d", caller.hyperspaceControls)
	}
	if len(caller.alignmentPurposes) != 1 || caller.alignmentPurposes[0] != "HYPERSPACE_CHARGE" {
		t.Fatalf("alignmentPurposes=%v", caller.alignmentPurposes)
	}
}

func TestEliteHyperspaceJumpDoesNotConvertVisibleTargetInfrastructureFailureIntoDomainUnknown(t *testing.T) {
	caller := &hyperspaceJumpCaller{alignVisibleError: capture.Failure("capture_readback_failed", "region readback failed", errors.New("HRESULT 0x80070057"))}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err == nil || !strings.Contains(err.Error(), "region readback failed") || !strings.Contains(err.Error(), "HRESULT 0x80070057") {
		t.Fatalf("error=%v", err)
	}
	if caller.hyperspaceControls != 0 {
		t.Fatalf("infrastructure failure must block FSD control, controls=%d", caller.hyperspaceControls)
	}
}

func TestEliteHyperspaceJumpPassesSupercruiseModeToOcclusionClearance(t *testing.T) {
	caller := &hyperspaceJumpCaller{forceObstruction: true}
	inputs := hyperspaceJumpInputs()
	inputs["startMode"] = "SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), inputs, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.clearStartModes) != 1 || caller.clearStartModes[0] != "SUPERCRUISE" {
		t.Fatalf("clear start modes=%v", caller.clearStartModes)
	}
}

func TestEliteHyperspaceJumpChecksSubstantialStellarCoverageAndHeatAfterAlignment(t *testing.T) {
	caller := &hyperspaceJumpCaller{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadEliteHyperspaceJumpPackage(t), hyperspaceJumpInputs(), caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	if caller.occlusionCalls != 3 {
		t.Fatalf("stellar CV must run before Compass, after Compass, and after visible alignment, calls=%d", caller.occlusionCalls)
	}
	if caller.hyperspaceControls != 1 {
		t.Fatalf("expected FSD control only after successful visible alignment, controls=%d", caller.hyperspaceControls)
	}
	wantPrefix := []string{
		"elite-dangerous/hyperspace-target-occlusion",
		"elite-dangerous/align-station-target",
		"elite-dangerous/hyperspace-target-occlusion",
		"elite-dangerous/align-visible-target",
		"elite-dangerous/hyperspace-target-occlusion",
		"elite-dangerous/ship-heat",
		"elite-dangerous/ship-heat",
		"elite-dangerous/ship-heat",
	}
	if len(caller.calls) < len(wantPrefix) {
		t.Fatalf("child calls=%v", caller.calls)
	}
	for index, want := range wantPrefix {
		if caller.calls[index] != want {
			t.Fatalf("child call %d=%q want %q; calls=%v", index, caller.calls[index], want, caller.calls)
		}
	}
}
