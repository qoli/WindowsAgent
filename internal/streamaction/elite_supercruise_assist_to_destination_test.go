package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type supercruiseAssistDestinationCaller struct {
	panelStates            []string
	panelIndex             int
	navigationRegions      []json.RawMessage
	navigationIndex        int
	assistRegions          []json.RawMessage
	assistIndex            int
	flightStates           []string
	flightIndex            int
	speedStates            []string
	speedIndex             int
	controls               []string
	throttles              []int
	supercruiseKeys        int
	alignmentCalls         int
	alignmentInputs        []map[string]any
	visibleAlignmentCalls  int
	assistOwnershipActive  bool
	flightInputAfterAssist []string
	supercruiseHUDStates   []string
	supercruiseHUDIndex    int
}

func focusedPixels(focused bool) []any {
	pixels := make([]any, 16)
	value := uint32(0)
	if focused {
		value = 0xFF7700
	}
	for index := range pixels {
		pixels[index] = value
	}
	return pixels
}

func textRegionRaw(text string, y float64, focused bool) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"regions": []any{map[string]any{
			"text": text, "detectionConfidence": 0.96, "recognitionConfidence": 0.97,
			"referencePoints": []any{
				map[string]any{"x": 500.0, "y": y}, map[string]any{"x": 680.0, "y": y},
				map[string]any{"x": 680.0, "y": y + 24}, map[string]any{"x": 500.0, "y": y + 24},
			},
			"leftContext": map[string]any{"w": 4, "h": 4, "pixels": focusedPixels(focused)},
		}},
	})
	return value
}

func (c *supercruiseAssistDestinationCaller) recordFlightInput(input string) {
	if c.assistOwnershipActive {
		c.flightInputAfterAssist = append(c.flightInputAfterAssist, input)
	}
}

func (c *supercruiseAssistDestinationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/ship-status":
		return json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`), nil
	case "elite-dangerous/left-panel-tab-state":
		if c.panelIndex >= len(c.panelStates) {
			return nil, errors.New("unexpected panel observation")
		}
		state := c.panelStates[c.panelIndex]
		c.panelIndex++
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/navigation-list-text-regions":
		if c.navigationIndex >= len(c.navigationRegions) {
			return nil, errors.New("unexpected Navigation OCR observation")
		}
		value := c.navigationRegions[c.navigationIndex]
		c.navigationIndex++
		return value, nil
	case "elite-dangerous/lock-destination-text-regions":
		if c.assistIndex >= len(c.assistRegions) {
			return nil, errors.New("unexpected Assist OCR observation")
		}
		value := c.assistRegions[c.assistIndex]
		c.assistIndex++
		return value, nil
	case "elite-dangerous/ui-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		c.recordFlightInput("UI:" + control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/close-left-panel":
		return json.RawMessage(`{"completed":true}`), nil
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		c.recordFlightInput("THROTTLE")
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/align-station-target":
		c.alignmentCalls++
		c.alignmentInputs = append(c.alignmentInputs, map[string]any{
			"mode":            inputs["mode"],
			"targetMotion":    inputs["targetMotion"],
			"stopBeforeAlign": inputs["stopBeforeAlign"],
			"controlProfile":  inputs["controlProfile"],
		})
		return json.RawMessage(`{"schemaVersion":1,"task":"ALIGN_STATION_TARGET","completed":true,"sampleCount":3}`), nil
	case "elite-dangerous/align-visible-target":
		c.visibleAlignmentCalls++
		return nil, errors.New("supercruise Assist must not replace Compass feedback with visible-target search")
	case "elite-dangerous/supercruise-control":
		c.supercruiseKeys++
		c.recordFlightInput("FSD")
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"text":"fixture"}`), nil
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		if state == "SUPERCRUISE_ASSIST_ACTIVE" && c.flightIndex >= 4 {
			c.assistOwnershipActive = true
		}
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}})
	case "elite-dangerous/supercruise-hud-state":
		state := "INACTIVE"
		if c.supercruiseHUDIndex < len(c.supercruiseHUDStates) {
			state = c.supercruiseHUDStates[c.supercruiseHUDIndex]
			c.supercruiseHUDIndex++
		}
		return json.Marshal(map[string]any{"supercruiseHud": map[string]any{"state": state}})
	case "elite-dangerous/ship-speed":
		if c.speedIndex >= len(c.speedStates) {
			return nil, errors.New("unexpected ship-speed observation")
		}
		state := c.speedStates[c.speedIndex]
		c.speedIndex++
		if state == "STOPPED" {
			return json.RawMessage(`{"speed":{"state":"STOPPED","displayValue":0,"rawCandidate":0}}`), nil
		}
		return json.RawMessage(`{"speed":{"state":"MOVING","displayValue":42,"rawCandidate":42}}`), nil
	default:
		return nil, errors.New("unexpected Supercruise Assist child Action: " + id)
	}
}

func loadEliteSupercruiseAssistToDestinationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-assist-to-destination"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func successfulSupercruiseAssistCaller() *supercruiseAssistDestinationCaller {
	return &supercruiseAssistDestinationCaller{
		panelStates: []string{"ABSENT", "ABSENT", "NAVIGATION", "NAVIGATION", "NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		navigationRegions: []json.RawMessage{
			textRegionRaw("<NAV BEACON>", 400, true), textRegionRaw("<NAV BEACON>", 400, true),
		},
		assistRegions: []json.RawMessage{
			textRegionRaw("SUPERCRUISE ASSIST", 720, true), textRegionRaw("SUPERCRUISE ASSIST", 720, true),
		},
		flightStates: []string{
			"FSD_CHARGING", "SUPERCRUISE", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
			"UNKNOWN", "UNKNOWN", "UNKNOWN",
		},
		speedStates: []string{"STOPPED", "STOPPED", "STOPPED"},
	}
}

func supercruiseAssistInputs() map[string]any {
	return map[string]any{
		"targetName": "NAV BEACON", "targetLocked": true, "normalSpaceConfirmed": true,
		"autoThrottleConfirmed": true, "destinationMode": "DROP",
	}
}

func TestEliteSupercruiseAssistToDestinationHandsFlightToGameComputer(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"task":"SUPERCRUISE_ASSIST_TO_DESTINATION"`) ||
		!contains(string(output), `"agentFlightInputAfterAssistActive":false`) ||
		!contains(string(output), `"destinationMode":"DROP"`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.flightInputAfterAssist) != 0 {
		t.Fatalf("flight input after Assist ownership=%v", caller.flightInputAfterAssist)
	}
	if caller.alignmentCalls != 1 || caller.visibleAlignmentCalls != 0 {
		t.Fatalf("alignment calls compass=%d visible=%d", caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	if len(caller.alignmentInputs) != 1 ||
		caller.alignmentInputs[0]["mode"] != "ALIGN" ||
		caller.alignmentInputs[0]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[0]["stopBeforeAlign"] != false ||
		caller.alignmentInputs[0]["controlProfile"] != "NORMAL_SPACE" {
		t.Fatalf("initial alignment inputs=%v", caller.alignmentInputs)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "SELECT", "RIGHT", "SELECT", "FOCUS_LEFT_PANEL"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v", caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
	if len(caller.throttles) != 4 || caller.throttles[0] != 0 || caller.throttles[1] != 100 || caller.throttles[2] != 0 || caller.throttles[3] != 75 || caller.supercruiseKeys != 1 {
		t.Fatalf("throttles=%v supercruiseKeys=%d", caller.throttles, caller.supercruiseKeys)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"phase":"ASSIST_ACTIVE"`) || !contains(joined, `"phase":"COMPLETED"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteSupercruiseAssistWaitsForNavigationBracketsAfterPanelOpen(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.navigationRegions = []json.RawMessage{
		textRegionRaw("HARRIS PORT", 400, true), textRegionRaw("HARRIS PORT", 400, true),
		textRegionRaw("<HARRIS PORT", 400, true), textRegionRaw("<HARRIS PORT", 400, true),
	}
	inputs := supercruiseAssistInputs()
	inputs["targetName"] = "HARRIS PORT"
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), inputs, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	joined := ""
	for _, payload := range reporter.payloads {
		joined += string(payload)
	}
	if !contains(joined, `"reason":"WAITING_FOR_NAVIGATION_LOCK_STABILIZATION_1_OF_3"`) {
		t.Fatalf("warm-up evidence missing from events=%s", joined)
	}
}

func TestEliteSupercruiseAssistReusesAlignmentActionWhenAssistRequiresAlignment(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	if caller.alignmentCalls != 2 {
		t.Fatalf("expected initial and Assist-required alignment calls, got %d", caller.alignmentCalls)
	}
	if caller.visibleAlignmentCalls != 0 {
		t.Fatalf("visible-target search replaced Compass feedback %d times", caller.visibleAlignmentCalls)
	}
	if len(caller.alignmentInputs) != 2 ||
		caller.alignmentInputs[0]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[0]["controlProfile"] != "NORMAL_SPACE" ||
		caller.alignmentInputs[1]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[1]["controlProfile"] != "SUPERCRUISE_ASSIST" {
		t.Fatalf("alignment inputs=%v", caller.alignmentInputs)
	}
	wantThrottles := []int{0, 100, 0, 75, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
}

func TestEliteSupercruiseAssistRejectsOrbitButtonWithoutSelectingIt(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.assistRegions = []json.RawMessage{
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, false),
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, false),
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, false),
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, false),
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "module may be absent or the detail layout is unsupported") {
		t.Fatalf("error=%v", err)
	}
	selects := 0
	for _, control := range caller.controls {
		if control == "SELECT" {
			selects++
		}
	}
	if selects != 1 {
		t.Fatalf("DROP workflow selected an ORBIT action: controls=%v", caller.controls)
	}
}

func TestEliteSupercruiseAssistHandsOrbitFlightToGameWithoutClaimingArrival(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.assistRegions = []json.RawMessage{
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, true),
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, true),
	}
	caller.navigationRegions = []json.RawMessage{
		textRegionRaw("<LTT 11244 A 2>", 400, true),
		textRegionRaw("<LTT 11244 A 2>", 400, true),
	}
	caller.flightStates = []string{"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE"}
	caller.supercruiseHUDStates = []string{"ACTIVE", "ACTIVE"}
	inputs := map[string]any{
		"targetName": "LTT 11244 A 2", "targetLocked": true,
		"normalSpaceConfirmed": false, "supercruiseConfirmed": true,
		"autoThrottleConfirmed": true, "destinationMode": "ORBIT_HANDOFF",
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), inputs, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"destinationMode":"ORBIT_HANDOFF"`) ||
		!contains(string(output), `"finalPhase":"ASSIST_HANDOFF"`) ||
		!contains(string(output), `"finalSpeed":null`) {
		t.Fatalf("output=%s", output)
	}
	selects := 0
	for _, control := range caller.controls {
		if control == "SELECT" {
			selects++
		}
	}
	if selects != 2 {
		t.Fatalf("ORBIT workflow did not open the target and select Assist: controls=%v", caller.controls)
	}
}

func TestEliteSupercruiseAssistInterruptionFailsWithoutManualFlightFallback(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
	}
	caller.speedStates = nil
	for index := 0; index < 30; index++ {
		caller.flightStates = append(caller.flightStates, "UNKNOWN")
		caller.speedStates = append(caller.speedStates, "MOVING")
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "ASSIST_INTERRUPTED") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.flightInputAfterAssist) != 1 || caller.flightInputAfterAssist[0] != "THROTTLE" {
		t.Fatalf("unexpected post-Assist input=%v", caller.flightInputAfterAssist)
	}
	if caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("failure compensation did not end at 0%%: throttles=%v", caller.throttles)
	}
}

func TestEliteSupercruiseAssistEntryUnknownIsBoundedAndStopsShip(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = make([]string, 30)
	for index := range caller.flightStates {
		caller.flightStates[index] = "UNKNOWN"
	}
	caller.speedStates = nil
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FSD charging followed by Supercruise entry was not visually confirmed") {
		t.Fatalf("error=%v", err)
	}
	if caller.flightIndex != 30 {
		t.Fatalf("flight observations=%d", caller.flightIndex)
	}
	if len(caller.throttles) != 3 || caller.throttles[0] != 0 || caller.throttles[1] != 100 || caller.throttles[2] != 0 {
		t.Fatalf("failure compensation did not bound movement: throttles=%v", caller.throttles)
	}
}

func TestEliteSupercruiseAssistRestoresThrottleAtChargedHandoff(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_THROTTLE_UP_REQUIRED", "SUPERCRUISE", "SUPERCRUISE",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	wantThrottles := []int{0, 100, 100, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
}
