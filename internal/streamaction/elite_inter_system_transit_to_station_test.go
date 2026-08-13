package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type interSystemTransitCaller struct {
	hyperspaceStates   []string
	hyperspaceIndex    int
	throttles          []int64
	calls              []string
	systemLocks        int
	hudCalls           int
	targetCalls        int
	alignProfiles      []string
	jumpStartModes     []string
	jumpTargetLocks    []bool
	failJump           bool
	journalCalls       int
	journalArrival     bool
	resumeJournal      bool
	hyperspaceControls int
	occlusionStates    []string
	occlusionIndex     int
	occlusionEscapes   int
}

func (c *interSystemTransitCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	c.calls = append(c.calls, id)
	switch id {
	case "elite-dangerous/plot-route-to-system":
		if inputs["targetSystem"] != "NLTT 8084" || inputs["maxJumps"] != int64(1) {
			return nil, errors.New("unexpected one-hop route request")
		}
		return json.RawMessage(`{"completed":true,"result":"PLOTTED","routeId":"2026-08-12T00:00:00Z:123:1","jumpCount":1}`), nil
	case "elite-dangerous/hyperspace-jump-to-system":
		c.jumpStartModes = append(c.jumpStartModes, inputs["startMode"].(string))
		c.jumpTargetLocks = append(c.jumpTargetLocks, inputs["targetLockConfirmed"].(bool))
		if c.failJump {
			return nil, errors.New("FSD charging followed by stable hyperspace cockpit absence was not confirmed")
		}
		return json.RawMessage(`{"completed":true,"finalPhase":"ARRIVED_IN_SUPERCRUISE","arrivalEvidence":"JOURNAL_FSDJUMP","arrivalBrakeSent":true,"hyperspaceChargingConfirmed":true,"hyperspaceTransitConfirmed":true,"cockpitReturnConfirmations":2,"supercruiseHudConfirmations":2,"initialAlignment":{"coarseSamples":3,"fineSamples":3},"recoveryAlignment":null,"sampleCount":7}`), nil
	case "elite-dangerous/select-and-lock-destination":
		c.systemLocks++
		return json.RawMessage(`{"targetLocked":true,"result":"ACQUIRED"}`), nil
	case "elite-dangerous/align-station-target":
		profile, _ := inputs["controlProfile"].(string)
		c.alignProfiles = append(c.alignProfiles, profile)
		return json.RawMessage(`{"sampleCount":3}`), nil
	case "elite-dangerous/align-visible-target":
		return json.RawMessage(`{"sampleCount":3}`), nil
	case "elite-dangerous/hyperspace-state":
		if c.hyperspaceIndex >= len(c.hyperspaceStates) {
			return nil, errors.New("unexpected hyperspace-state observation")
		}
		state := c.hyperspaceStates[c.hyperspaceIndex]
		c.hyperspaceIndex++
		flight := "UNKNOWN"
		cockpit := "PRESENT"
		if state == "FSD_CHARGING" {
			flight = "FSD_CHARGING"
		}
		if state == "ALIGNMENT_REQUIRED" {
			flight = "FSD_ALIGNMENT_REQUIRED"
		}
		if state == "COCKPIT_ABSENT" {
			cockpit = "ABSENT"
		}
		return json.Marshal(map[string]any{"hyperspaceState": map[string]any{
			"state": state, "flightStatus": flight, "promptText": "", "cockpitHud": map[string]any{"state": cockpit},
		}})
	case "elite-dangerous/hyperspace-control":
		c.hyperspaceControls++
		return json.RawMessage(`{"control":"HyperSuperCombination"}`), nil
	case "elite-dangerous/set-throttle":
		percent, _ := inputs["percent"].(int64)
		c.throttles = append(c.throttles, percent)
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/supercruise-hud-state":
		c.hudCalls++
		return json.RawMessage(`{"supercruiseHud":{"state":"ACTIVE"}}`), nil
	case "elite-dangerous/filesystem/journal-navigation-tail":
		c.journalCalls++
		if c.resumeJournal {
			return json.RawMessage(`{"state":"AVAILABLE","events":[{"timestamp":"2026-08-12T07:56:44Z","event":"FSDJump","StarSystem":"Nltt 8084","SystemAddress":123,"JumpType":null,"RemainingJumpsInRoute":null}]}`), nil
		}
		if c.journalArrival && c.journalCalls > 1 {
			return json.RawMessage(`{"state":"AVAILABLE","events":[{"timestamp":"2026-08-10T11:03:03Z","event":"FSDJump","StarSystem":"Acihaut","SystemAddress":123,"JumpType":null,"RemainingJumpsInRoute":null}]}`), nil
		}
		return json.RawMessage(`{"state":"AVAILABLE","events":[{"timestamp":"2026-08-10T11:00:00Z","event":"FSDTarget","StarSystem":null,"SystemAddress":null,"JumpType":null,"RemainingJumpsInRoute":1}]}`), nil
	case "elite-dangerous/hyperspace-target-occlusion":
		state := "CLEAR"
		if c.occlusionIndex < len(c.occlusionStates) {
			state = c.occlusionStates[c.occlusionIndex]
			c.occlusionIndex++
		}
		ratio := 0.01
		if state != "CLEAR" {
			ratio = 0.8
		}
		return json.Marshal(map[string]any{"occlusion": map[string]any{"state": state, "brightRatio": ratio, "warmOrangeRatio": ratio, "stellarCoverageRatio": ratio, "centerCoverageRatio": ratio, "directionConfidence": 0.5, "recommendedControl": "PITCH_UP"}})
	case "elite-dangerous/clear-hyperspace-occlusion":
		c.occlusionEscapes++
		return json.RawMessage(`{"completed":true,"turnCount":4,"finalOcclusionState":"CLEAR","finalSupercruiseConfirmed":true}`), nil
	case "elite-dangerous/supercruise-target-position":
		c.targetCalls++
		return json.RawMessage(`{"target":{"state":"DETECTED","reason":"TARGET_LABEL_TO_MARKER_OFFSET_APPLIED"}}`), nil
	case "elite-dangerous/supercruise-assist-to-destination":
		if inputs["supercruiseConfirmed"] != true || inputs["normalSpaceConfirmed"] != false {
			return nil, errors.New("hyperspace arrival must resume existing Supercruise")
		}
		return json.RawMessage(`{"completed":true}`), nil
	case "elite-dangerous/dock-at-station":
		return json.RawMessage(`{"completed":true,"finalPhase":"VISUAL_CONFIRMATION_REQUIRED"}`), nil
	default:
		return nil, errors.New("unexpected inter-system child Action: " + id)
	}
}

func interSystemTransitPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "inter-system-transit-to-station"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func interSystemInputs() map[string]any {
	return map[string]any{
		"destinationSystem":             "NLTT 8084",
		"destinationStation":            "SURAYEV HUB",
		"startMode":                     "NORMAL_SPACE",
		"normalSpaceConfirmed":          true,
		"supercruiseConfirmed":          false,
		"stationCompatibilityConfirmed": true,
		"autoThrottleConfirmed":         true,
	}
}

func TestEliteInterSystemTransitAcceptsExplicitSupercruiseStart(t *testing.T) {
	pkg, err := Load(interSystemTransitPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{hyperspaceStates: []string{
		"COCKPIT_PRESENT", "FSD_CHARGING", "COCKPIT_ABSENT", "COCKPIT_ABSENT",
		"COCKPIT_PRESENT", "COCKPIT_PRESENT",
	}}
	inputs := interSystemInputs()
	inputs["startMode"] = "SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.jumpStartModes) != 1 || caller.jumpStartModes[0] != "SUPERCRUISE" {
		t.Fatalf("jumpStartModes=%v", caller.jumpStartModes)
	}
	if len(caller.jumpTargetLocks) != 1 || !caller.jumpTargetLocks[0] {
		t.Fatalf("jumpTargetLocks=%v", caller.jumpTargetLocks)
	}
}

func TestEliteInterSystemTransitComposesVisualSingleHopAndDocking(t *testing.T) {
	pkg, err := Load(interSystemTransitPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{hyperspaceStates: []string{
		"COCKPIT_PRESENT",
		"FSD_CHARGING", "COCKPIT_PRESENT", "COCKPIT_ABSENT", "COCKPIT_ABSENT",
		"COCKPIT_ABSENT", "COCKPIT_PRESENT", "COCKPIT_ABSENT", "COCKPIT_PRESENT", "COCKPIT_PRESENT",
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, interSystemInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"VISUAL_CONFIRMATION_REQUIRED"`) ||
		!contains(string(output), `"routeId":"2026-08-12T00:00:00Z:123:1"`) ||
		!contains(string(output), `"destinationSystem":"NLTT 8084"`) ||
		!contains(string(output), `"destinationStation":"SURAYEV HUB"`) ||
		caller.systemLocks != 1 || caller.hudCalls != 0 || caller.targetCalls != 0 {
		t.Fatalf("output=%s locks=%d hud=%d target=%d", output, caller.systemLocks, caller.hudCalls, caller.targetCalls)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"PLOTTING_ROUTE", "ROUTE_READY", "SYSTEM_LOCKED", "FSD_CHARGING", "HYPERSPACE_TRANSIT", "DESTINATION_SYSTEM_CONFIRMED", "STATION_LOCKED", "SUPERCRUISE_TO_STATION", "DOCKING", "VISUAL_CONFIRMATION_REQUIRED"} {
		if !contains(joined, phase) {
			t.Fatalf("missing phase %s in %s", phase, joined)
		}
	}
}

func TestEliteInterSystemTransitResumesAfterExactDestinationFSDJump(t *testing.T) {
	pkg, err := Load(interSystemTransitPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{resumeJournal: true}
	inputs := interSystemInputs()
	inputs["startMode"] = "ARRIVED_SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"destinationSystemArrivalEvidence":"JOURNAL_FSDJUMP_RESUME"`) ||
		!contains(string(output), `"routeId":"RESUME:2026-08-12T07:56:44Z"`) {
		t.Fatalf("output=%s", output)
	}
	for _, id := range caller.calls {
		if id == "elite-dangerous/plot-route-to-system" || id == "elite-dangerous/hyperspace-jump-to-system" {
			t.Fatalf("resume repeated completed inter-System phase: %s", id)
		}
	}
	if caller.systemLocks != 1 || caller.hudCalls != 2 {
		t.Fatalf("station locks=%d hudCalls=%d", caller.systemLocks, caller.hudCalls)
	}
}

func TestEliteInterSystemTransitResumeAcceptsCanonicalJournalCaseForUppercaseInput(t *testing.T) {
	pkg, err := Load(interSystemTransitPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{resumeJournal: true}
	inputs := interSystemInputs()
	inputs["destinationSystem"] = "NLTT 8084"
	inputs["startMode"] = "ARRIVED_SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"destinationSystemArrivalEvidence":"JOURNAL_FSDJUMP_RESUME"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteInterSystemTransitFailsWithoutChargingAndUsesOnlyZeroCompensation(t *testing.T) {
	pkg, err := Load(interSystemTransitPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{failJump: true}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, interSystemInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FSD charging followed by stable hyperspace cockpit absence was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if !equalInt64s(caller.throttles, []int64{0}) {
		t.Fatalf("failure compensation throttles=%v", caller.throttles)
	}
	for _, id := range caller.calls {
		if id == "elite-dangerous/supercruise-control" || id == "elite-dangerous/filesystem/status" {
			t.Fatalf("forbidden fallback Action called: %s", id)
		}
	}
}

func equalInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func joinEventPhases(payloads []json.RawMessage) string {
	joined := ""
	for _, payload := range payloads {
		if len(joined) != 0 {
			joined += ","
		}
		joined += string(payload)
	}
	return joined
}

type hyperspaceStateCaller struct {
	flightState  string
	cockpitState string
}

func (c *hyperspaceStateCaller) Call(_ context.Context, id string, _ map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"text":"PRESS TO ABORT"}`), nil
	case "elite-dangerous/flight-status":
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": c.flightState}})
	case "elite-dangerous/cockpit-hud-presence":
		return json.Marshal(map[string]any{"cockpitHud": map[string]any{"state": c.cockpitState, "orangePixelCount": 0, "chargeCyanPixelCount": 0, "hudPixelCount": 0, "minimumHudPixels": 150}, "profile": map[string]any{"capturedAt": "2026-08-10T00:00:00Z"}})
	default:
		return nil, errors.New("unexpected hyperspace-state child Action: " + id)
	}
}

func TestEliteHyperspaceStatePreservesSingleFrameEvidence(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "hyperspace-state"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		flight, cockpit, want string
	}{
		{flight: "FSD_CHARGING", cockpit: "PRESENT", want: "FSD_CHARGING"},
		{flight: "FSD_ALIGNMENT_REQUIRED", cockpit: "PRESENT", want: "ALIGNMENT_REQUIRED"},
		{flight: "UNKNOWN", cockpit: "ABSENT", want: "COCKPIT_ABSENT"},
		{flight: "UNKNOWN", cockpit: "PRESENT", want: "COCKPIT_PRESENT"},
	} {
		output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
			context.Background(), pkg, map[string]any{}, &hyperspaceStateCaller{flightState: test.flight, cockpitState: test.cockpit}, &fixtureReporter{},
		)
		if err != nil || !contains(string(output), `"state":"`+test.want+`"`) {
			t.Fatalf("flight=%s cockpit=%s output=%s error=%v", test.flight, test.cockpit, output, err)
		}
	}
}
