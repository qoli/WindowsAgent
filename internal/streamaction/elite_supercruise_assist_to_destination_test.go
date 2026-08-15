package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type supercruiseAssistDestinationCaller struct {
	shipStatuses           []json.RawMessage
	shipStatusIndex        int
	panelStates            []string
	panelIndex             int
	navigationRegions      []json.RawMessage
	navigationIndex        int
	assistRegions          []json.RawMessage
	assistIndex            int
	uiSnapshotStates       []string
	uiSnapshotIndex        int
	uiSnapshotPending      bool
	uiSnapshotTaken        bool
	disableUISnapshot      bool
	postPanelStates        []string
	postPanelIndex         int
	postPanelPending       bool
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
	visibleAlignmentInputs []map[string]any
	visibleAlignmentErrors []error
	alignmentSequence      []string
	assistOwnershipActive  bool
	flightInputAfterAssist []string
	supercruiseHUDStates   []string
	supercruiseHUDIndex    int
	speedErrors            []error
	lineOfSightCalls       int
	closeNavigationCalls   int
	closeNavigationOutput  json.RawMessage
	orbitalScaleDetected   bool
	orbitalScaleDetectAt   int
	orbitalScaleCalls      int
	humanTakeoverCalls     int
	humanTakeoverOutput    json.RawMessage
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
	case "elite-dangerous/orbital-scale-gauge-state":
		c.orbitalScaleCalls++
		if c.orbitalScaleDetected && (c.orbitalScaleDetectAt == 0 || c.orbitalScaleCalls >= c.orbitalScaleDetectAt) {
			return json.RawMessage(`{"schemaVersion":2,"gauge":{"state":"DETECTED","confidence":0.91,"threshold":0.75,"reason":"ORBITAL_VERTICAL_SCALE_GEOMETRY_CONFIRMED"},"evidence":{}}`), nil
		}
		return json.RawMessage(`{"schemaVersion":2,"gauge":{"state":"ABSENT","confidence":0.12,"threshold":0.75,"reason":"ORBITAL_VERTICAL_SCALE_GEOMETRY_NOT_CONFIRMED"},"evidence":{}}`), nil
	case "elite-dangerous/pause-at-exit-for-human-takeover":
		c.humanTakeoverCalls++
		if c.humanTakeoverOutput != nil {
			return c.humanTakeoverOutput, nil
		}
		return json.RawMessage(`{"schemaVersion":2,"task":"PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER","completed":true,"pauseMenuConfirmed":true,"exitFocused":true,"firstExitSelectSent":true,"exitDestinationMenuConfirmed":true,"exitToMainMenuFocused":true,"exitToMainMenuSelectSent":true,"mainMenuConfirmed":true,"openAttempts":1,"postExitSamples":7,"finalObservation":{"state":"MAIN_MENU","reason":"MAIN_MENU_EXACT_ANCHORS_CONFIRMED","focusFillRatio":0,"rawTexts":["CONTINUE","SOCIAL","OPTIONS"]}}`), nil
	case "elite-dangerous/ship-status":
		if c.shipStatusIndex < len(c.shipStatuses) {
			value := c.shipStatuses[c.shipStatusIndex]
			c.shipStatusIndex++
			return value, nil
		}
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
	case "elite-dangerous/close-navigation-detail":
		c.closeNavigationCalls++
		c.postPanelPending = true
		if c.closeNavigationOutput != nil {
			return c.closeNavigationOutput, nil
		}
		return json.RawMessage(`{"schemaVersion":1,"backSent":true,"listConfirmed":true,"panelClosed":true,"finalState":"ABSENT"}`), nil
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		c.alignmentSequence = append(c.alignmentSequence, "THROTTLE_"+strconv.Itoa(int(percent)))
		if percent == 0 && !c.disableUISnapshot && !c.uiSnapshotTaken && (c.supercruiseKeys > 0 || c.supercruiseHUDIndex >= 2) {
			c.uiSnapshotPending = true
			c.uiSnapshotTaken = true
		}
		c.recordFlightInput("THROTTLE")
		return json.Marshal(map[string]any{"control": map[int64]string{0: "SetSpeedZero", 100: "SetSpeed100"}[percent]})
	case "elite-dangerous/align-station-target":
		c.alignmentCalls++
		c.alignmentSequence = append(c.alignmentSequence, "COMPASS")
		c.alignmentInputs = append(c.alignmentInputs, map[string]any{
			"targetName":       inputs["targetName"],
			"mode":             inputs["mode"],
			"targetMotion":     inputs["targetMotion"],
			"alignmentPurpose": inputs["alignmentPurpose"],
			"stopBeforeAlign":  inputs["stopBeforeAlign"],
			"controlProfile":   inputs["controlProfile"],
		})
		targetMotion, _ := inputs["targetMotion"].(string)
		if inputs["targetName"] == "NAV BEACON" {
			targetMotion = "MOVING"
		}
		return json.Marshal(map[string]any{"schemaVersion": 1, "task": "ALIGN_STATION_TARGET", "completed": true, "sampleCount": 3, "targetMotion": targetMotion})
	case "elite-dangerous/align-visible-target":
		c.visibleAlignmentCalls++
		c.alignmentSequence = append(c.alignmentSequence, "VISIBLE")
		c.visibleAlignmentInputs = append(c.visibleAlignmentInputs, map[string]any{
			"targetName":          inputs["targetName"],
			"stopBeforeAlign":     inputs["stopBeforeAlign"],
			"centerHintConfirmed": inputs["centerHintConfirmed"],
			"positionSource":      inputs["positionSource"],
			"heatPolicy":          inputs["heatPolicy"],
		})
		if len(c.visibleAlignmentErrors) > 0 {
			err := c.visibleAlignmentErrors[0]
			c.visibleAlignmentErrors = c.visibleAlignmentErrors[1:]
			if err != nil {
				return nil, err
			}
		}
		return json.RawMessage(`{"schemaVersion":1,"task":"ALIGN_VISIBLE_TARGET","completed":true,"sampleCount":4}`), nil
	case "elite-dangerous/clear-supercruise-assist-line-of-sight":
		c.lineOfSightCalls++
		return json.RawMessage(`{"schemaVersion":2,"task":"CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT","completed":true,"targetName":"NAV BEACON","control":"YAW_RIGHT","turnPulses":4,"sphereExitConfirmed":true,"separationDurationMs":30000,"separationSamples":60,"finalFlightStatus":"SUPERCRUISE","sampleCount":68}`), nil
	case "elite-dangerous/supercruise-control":
		c.supercruiseKeys++
		c.alignmentSequence = append(c.alignmentSequence, "FSD")
		c.recordFlightInput("FSD")
		return json.RawMessage(`{"control":"Supercruise"}`), nil
	case "elite-dangerous/flight-prompt-text":
		return json.RawMessage(`{"text":"fixture"}`), nil
	case "elite-dangerous/flight-status":
		if c.uiSnapshotPending && c.uiSnapshotIndex < len(c.uiSnapshotStates) {
			state := c.uiSnapshotStates[c.uiSnapshotIndex]
			c.uiSnapshotIndex++
			c.uiSnapshotPending = false
			return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}, "source": map[string]any{"text": "ALIGN WITH TARGET DESTINATION"}})
		}
		if c.postPanelPending && c.postPanelIndex < len(c.postPanelStates) {
			state := c.postPanelStates[c.postPanelIndex]
			c.postPanelIndex++
			c.postPanelPending = false
			return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}, "source": map[string]any{"text": "ALIGN WITH TARGET DESTINATION"}})
		}
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		if state == "SUPERCRUISE_ASSIST_ACTIVE" && c.flightIndex >= 4 {
			c.assistOwnershipActive = true
		}
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}, "source": map[string]any{"text": "fixture"}})
	case "elite-dangerous/supercruise-hud-state":
		state := "INACTIVE"
		if c.supercruiseHUDIndex < len(c.supercruiseHUDStates) {
			state = c.supercruiseHUDStates[c.supercruiseHUDIndex]
			c.supercruiseHUDIndex++
		}
		return json.Marshal(map[string]any{"supercruiseHud": map[string]any{"state": state}})
	case "elite-dangerous/ship-speed":
		if len(c.speedErrors) > 0 {
			err := c.speedErrors[0]
			c.speedErrors = c.speedErrors[1:]
			if err != nil {
				return nil, err
			}
		}
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

func TestEliteSupercruiseAssistSkipsTransientShipSpeedWGCFailure(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = append(caller.flightStates, "UNKNOWN")
	caller.speedErrors = []error{errors.New("capture OCR Action region: persistent WGC worker region capture: persistent region capture failed: HRESULT 0x80070057")}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
}

func TestEliteSupercruiseAssistPreflightRetriesUnknownUntilTwoSafeObservations(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.shipStatuses = []json.RawMessage{
		json.RawMessage(`{"shipStatus":{"massLock":{"state":"UNKNOWN"},"landingGear":{"state":"UNKNOWN"},"cargoScoop":{"state":"UNKNOWN"}}}`),
		json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`),
		json.RawMessage(`{"shipStatus":{"massLock":{"state":"OFF"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`),
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"SHIP_STATUS_INCOMPLETE_ATTEMPT_1_OF_4"`) || !contains(joined, `"reason":"SHIP_STATUS_SAFE_2_OF_2"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteSupercruiseAssistPreflightFailsImmediatelyOnKnownUnsafeState(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.shipStatuses = []json.RawMessage{
		json.RawMessage(`{"shipStatus":{"massLock":{"state":"ON"},"landingGear":{"state":"OFF"},"cargoScoop":{"state":"OFF"}}}`),
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "requires visual Mass Lock") {
		t.Fatalf("error=%v", err)
	}
	if caller.shipStatusIndex != 1 || len(caller.throttles) != 0 {
		t.Fatalf("ship observations=%d throttles=%v", caller.shipStatusIndex, caller.throttles)
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

func TestEliteSupercruiseAssistNavigationDetailFailureCompensationUsesFiveSecondBudget(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-assist-to-destination"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "main.star"))
	if err != nil {
		t.Fatal(err)
	}
	registration := `action.on_failure(id="elite-dangerous/close-navigation-detail", inputs={}, timeout_milliseconds=5000)`
	if strings.Count(string(source), registration) != 1 || strings.Contains(string(source), "timeout_milliseconds=15000") {
		t.Fatalf("close-navigation-detail failure budget drifted: source=%s", source)
	}
}

func successfulSupercruiseAssistCaller() *supercruiseAssistDestinationCaller {
	return &supercruiseAssistDestinationCaller{
		panelStates: []string{"ABSENT", "ABSENT", "NAVIGATION", "NAVIGATION", "NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		navigationRegions: []json.RawMessage{
			textRegionRaw("<NAV BEACON>", 400, true), textRegionRaw("<NAV BEACON>", 400, true),
		},
		assistRegions: []json.RawMessage{
			textRegionRaw("ACTIVATE SUPERCRUISE ASSIST", 720, true), textRegionRaw("ACTIVATE SUPERCRUISE ASSIST", 720, true),
		},
		uiSnapshotStates: []string{"FSD_ALIGNMENT_REQUIRED"},
		postPanelStates:  []string{"FSD_ALIGNMENT_REQUIRED"},
		flightStates: []string{
			"FSD_CHARGING", "SUPERCRUISE", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
			"UNKNOWN", "UNKNOWN", "UNKNOWN",
		},
		speedStates: []string{"STOPPED", "STOPPED", "STOPPED"},
	}
}

func TestEliteSupercruiseAssistDoesNotSelectAlreadyActiveContextAction(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.assistRegions = []json.RawMessage{
		textRegionRaw("DEACTIVATE SUPERCRUISE ASSIST", 720, true),
		textRegionRaw("DEACTIVATE SUPERCRUISE ASSIST", 720, true),
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	selects := 0
	for _, control := range caller.controls {
		if control == "SELECT" {
			selects++
		}
	}
	if selects != 1 {
		t.Fatalf("already-active Assist must only select the Navigation row: controls=%v", caller.controls)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"assistButtonState":"ACTIVE"`) ||
		!contains(joined, `"reason":"ASSIST_ALREADY_ACTIVE_NO_SELECT"`) {
		t.Fatalf("events=%s", joined)
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
	if caller.alignmentCalls != 1 || caller.visibleAlignmentCalls != 1 {
		t.Fatalf("alignment calls compass=%d visible=%d", caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	if len(caller.alignmentInputs) != 1 ||
		caller.alignmentInputs[0]["mode"] != "ALIGN" ||
		caller.alignmentInputs[0]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[0]["alignmentPurpose"] != "HYPERSPACE_CHARGE" ||
		caller.alignmentInputs[0]["stopBeforeAlign"] != false ||
		caller.alignmentInputs[0]["controlProfile"] != "NORMAL_SPACE" {
		t.Fatalf("initial alignment inputs=%v", caller.alignmentInputs)
	}
	if len(caller.visibleAlignmentInputs) != 1 ||
		caller.visibleAlignmentInputs[0]["targetName"] != "NAV BEACON" ||
		caller.visibleAlignmentInputs[0]["centerHintConfirmed"] != true ||
		caller.visibleAlignmentInputs[0]["positionSource"] != "DESTINATION" ||
		caller.visibleAlignmentInputs[0]["heatPolicy"] != "STRICT" {
		t.Fatalf("initial visible alignment inputs=%v", caller.visibleAlignmentInputs)
	}
	if !strings.Contains(strings.Join(caller.alignmentSequence, ","), "THROTTLE_0,COMPASS,VISIBLE,FSD,THROTTLE_100") {
		t.Fatalf("initial alignment ordering=%v", caller.alignmentSequence)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "SELECT", "RIGHT", "SELECT"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v", caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
	if caller.closeNavigationCalls != 1 {
		t.Fatalf("Navigation detail close calls=%d", caller.closeNavigationCalls)
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
	if !contains(joined, `"phase":"LOCATING_ASSIST"`) ||
		!contains(joined, `"flightStatus":"FSD_ALIGNMENT_REQUIRED"`) ||
		!contains(joined, `"flightStatusSource":"PRE_NAVIGATION_SNAPSHOT"`) ||
		!contains(joined, `"flightPromptText":"ALIGN WITH TARGET DESTINATION"`) {
		t.Fatalf("Assist UI events did not retain fresh flight context: events=%s", joined)
	}
	if !contains(joined, `"phase":"CLOSING_PANEL"`) ||
		!contains(joined, `"flightStatusSource":"CURRENT_FRAME"`) {
		t.Fatalf("panel-close event did not resume current-frame flight context: events=%s", joined)
	}
	if !contains(joined, `"reason":"INITIAL_ALIGNMENT_PAIR_COMPLETED:3+4"`) ||
		!contains(joined, `"lastCommand":"ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET"`) {
		t.Fatalf("initial pair summary missing from events=%s", joined)
	}
}

func TestEliteSupercruiseAssistChargingAlignmentRequiresCompassThenVisibleBeforeRestoringThrottle(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.disableUISnapshot = true
	caller.postPanelStates = []string{"SUPERCRUISE"}
	caller.flightStates = []string{
		"FSD_ALIGNMENT_REQUIRED", "SUPERCRUISE", "SUPERCRUISE",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.alignmentCalls != 2 || caller.visibleAlignmentCalls != 2 {
		t.Fatalf("entry alignment calls compass=%d visible=%d", caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	wantSequence := "THROTTLE_0,COMPASS,VISIBLE,FSD,THROTTLE_100,THROTTLE_0,COMPASS,VISIBLE,THROTTLE_100"
	if !strings.Contains(strings.Join(caller.alignmentSequence, ","), wantSequence) {
		t.Fatalf("charging alignment ordering=%v want subsequence=%s", caller.alignmentSequence, wantSequence)
	}
	wantThrottles := []int{0, 100, 0, 100, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"CHARGING_ALIGNMENT_PAIR_COMPLETED:3+4"`) ||
		!contains(joined, `"lastCommand":"ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET:3+4+SET_THROTTLE_100"`) {
		t.Fatalf("charging pair evidence missing from events=%s", joined)
	}
}

func TestEliteSupercruiseAssistInitialVisibleFailureDoesNotStartFSD(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.visibleAlignmentErrors = []error{errors.New("initial visible alignment failed")}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "initial visible alignment failed") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseKeys != 0 {
		t.Fatalf("initial visible failure sent FSD: keys=%d", caller.supercruiseKeys)
	}
	for _, throttle := range caller.throttles {
		if throttle != 0 {
			t.Fatalf("initial visible failure authorized movement: throttles=%v", caller.throttles)
		}
	}
	if len(caller.throttles) < 2 || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("initial visible failure compensation missing: throttles=%v", caller.throttles)
	}
}

func TestEliteSupercruiseAssistChargingVisibleFailureDoesNotRestoreFullThrottle(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.visibleAlignmentErrors = []error{nil, errors.New("charging visible alignment failed")}
	caller.flightStates = []string{"FSD_ALIGNMENT_REQUIRED"}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "charging visible alignment failed") {
		t.Fatalf("error=%v", err)
	}
	if caller.supercruiseKeys != 1 || caller.alignmentCalls != 2 || caller.visibleAlignmentCalls != 2 {
		t.Fatalf("keys=%d compass=%d visible=%d", caller.supercruiseKeys, caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	wantThrottles := []int{0, 100, 0, 0}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
}

func TestEliteSupercruiseAssistDoesNotThrottleBeforeNavigationDetailIsClosed(t *testing.T) {
	for _, output := range []string{
		`{"schemaVersion":1,"backSent":true,"listConfirmed":true,"panelClosed":false,"finalState":"NAVIGATION"}`,
		`{"schemaVersion":1,"backSent":true,"listConfirmed":true,"panelClosed":true,"finalState":"NAVIGATION"}`,
		`{"schemaVersion":1,"backSent":true,"listConfirmed":true,"panelClosed":false,"finalState":"ABSENT"}`,
	} {
		t.Run(output, func(t *testing.T) {
			caller := successfulSupercruiseAssistCaller()
			caller.closeNavigationOutput = json.RawMessage(output)
			_, err := (Runner{Sleep: immediateSleep}).Run(
				context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
			)
			if err == nil || !contains(err.Error(), "Navigation detail did not restore the forward view") {
				t.Fatalf("error=%v", err)
			}
			for _, throttle := range caller.throttles {
				if throttle == 75 {
					t.Fatalf("75%% throttle was authorized before forward-view proof: throttles=%v", caller.throttles)
				}
			}
			if caller.closeNavigationCalls < 2 {
				t.Fatalf("detail close was not retained as failure compensation: calls=%d", caller.closeNavigationCalls)
			}
			if len(caller.throttles) == 0 || caller.throttles[len(caller.throttles)-1] != 0 {
				t.Fatalf("failure compensation did not end at 0%%: throttles=%v", caller.throttles)
			}
		})
	}
}

func TestEliteSupercruiseAssistBoundsWaitingForOwnershipEvidence(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err == nil || !contains(err.Error(), "ownership produced no ACTIVE, alignment, or line-of-sight Gate") {
		t.Fatalf("error=%v", err)
	}
	if caller.flightIndex != 8 {
		t.Fatalf("flight observations=%d", caller.flightIndex)
	}
	if len(caller.throttles) < 2 || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("ownership timeout did not end at 0%%: throttles=%v", caller.throttles)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"ASSIST_OWNERSHIP_TIMEOUT"`) ||
		!contains(joined, `"assistMissingSamples":6`) ||
		!contains(joined, `"reason":"NO_ASSIST_GATE_6_OF_6:`) {
		t.Fatalf("ownership timeout evidence missing: events=%s", joined)
	}
}

func TestEliteSupercruiseAssistDoesNotResetOwnershipTimeoutForOneFrameGateCandidates(t *testing.T) {
	for _, candidate := range []string{"FSD_ALIGNMENT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"} {
		t.Run(candidate, func(t *testing.T) {
			caller := successfulSupercruiseAssistCaller()
			caller.flightStates = []string{
				"FSD_CHARGING", "SUPERCRUISE",
				"UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN",
				candidate,
				"UNKNOWN",
			}
			_, err := (Runner{Sleep: immediateSleep}).Run(
				context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
			)
			if err == nil || !contains(err.Error(), "ownership produced no ACTIVE, alignment, or line-of-sight Gate") {
				t.Fatalf("error=%v", err)
			}
			if caller.alignmentCalls != 1 || caller.visibleAlignmentCalls != 1 || caller.lineOfSightCalls != 0 {
				t.Fatalf("candidate incorrectly entered recovery: compass=%d visible=%d lineOfSight=%d", caller.alignmentCalls, caller.visibleAlignmentCalls, caller.lineOfSightCalls)
			}
			if caller.throttles[len(caller.throttles)-1] != 0 {
				t.Fatalf("candidate timeout did not end at 0%%: throttles=%v", caller.throttles)
			}
		})
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

func TestEliteSupercruiseAssistRequiresCompassVisibleAndPromptClearBeforeRestoringThrottle(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
		"SUPERCRUISE", "SUPERCRUISE",
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
		t.Fatalf("expected initial and Assist-required Compass alignment calls, got %d", caller.alignmentCalls)
	}
	if caller.visibleAlignmentCalls != 2 {
		t.Fatalf("expected initial and Assist-required visible-target alignment calls, got %d", caller.visibleAlignmentCalls)
	}
	if len(caller.alignmentInputs) != 2 ||
		caller.alignmentInputs[0]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[0]["alignmentPurpose"] != "HYPERSPACE_CHARGE" ||
		caller.alignmentInputs[0]["controlProfile"] != "NORMAL_SPACE" ||
		caller.alignmentInputs[1]["targetMotion"] != "STATIC" ||
		caller.alignmentInputs[1]["alignmentPurpose"] != "VISIBLE_HANDOFF" ||
		caller.alignmentInputs[1]["controlProfile"] != "SUPERCRUISE_ASSIST" {
		t.Fatalf("alignment inputs=%v", caller.alignmentInputs)
	}
	if len(caller.visibleAlignmentInputs) != 2 ||
		caller.visibleAlignmentInputs[0]["targetName"] != "NAV BEACON" ||
		caller.visibleAlignmentInputs[0]["stopBeforeAlign"] != false ||
		caller.visibleAlignmentInputs[0]["centerHintConfirmed"] != true ||
		caller.visibleAlignmentInputs[1]["centerHintConfirmed"] != true ||
		caller.visibleAlignmentInputs[0]["positionSource"] != "DESTINATION" ||
		caller.visibleAlignmentInputs[0]["heatPolicy"] != "STRICT" {
		t.Fatalf("visible alignment inputs=%v", caller.visibleAlignmentInputs)
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

func TestEliteSupercruiseAssistRepeatsBothAlignmentsWhilePromptPersists(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
		"FSD_ALIGNMENT_REQUIRED",
		"UNKNOWN", "UNKNOWN",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s", output)
	}
	if caller.alignmentCalls != 3 || caller.visibleAlignmentCalls != 3 {
		t.Fatalf("expected initial Compass plus two complete correction cycles, compass=%d visible=%d", caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	wantThrottles := []int{0, 100, 0, 75, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("prompt-persistent cycle restored throttle early: throttles=%v", caller.throttles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"reason":"ALIGNMENT_PROMPT_STILL_PRESENT_AFTER_CYCLE:1"`) ||
		!contains(joined, `"reason":"ALIGNMENT_PROMPT_CLEAR_2_OF_2:CYCLE_2"`) {
		t.Fatalf("alignment Gate evidence missing from events=%s", joined)
	}
}

func TestEliteSupercruiseAssistPersistentAlignmentPromptFailsStopped(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
		"FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED", "FSD_ALIGNMENT_REQUIRED",
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "ALIGNMENT_PROMPT_PERSISTED") {
		t.Fatalf("error=%v", err)
	}
	if caller.alignmentCalls != 7 || caller.visibleAlignmentCalls != 7 {
		t.Fatalf("bounded cycles compass=%d visible=%d", caller.alignmentCalls, caller.visibleAlignmentCalls)
	}
	seventyFiveCommands := 0
	for _, throttle := range caller.throttles {
		if throttle == 75 {
			seventyFiveCommands++
		}
	}
	if seventyFiveCommands != 1 || caller.throttles[len(caller.throttles)-1] != 0 {
		t.Fatalf("persistent prompt must not restore 75%% after correction starts and must compensate to 0%%: throttles=%v", caller.throttles)
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

func TestEliteSupercruiseAssistOrbitModeWaitsForNearOrbitThenHandsToHuman(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.assistRegions = []json.RawMessage{
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, true),
		textRegionRaw("SUPERCRUISE ASSIST AND ORBIT", 720, true),
	}
	caller.navigationRegions = []json.RawMessage{
		textRegionRaw("<LTT 11244 A 2>", 400, true),
		textRegionRaw("<LTT 11244 A 2>", 400, true),
	}
	caller.flightStates = []string{"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE"}
	caller.speedStates = []string{"MOVING"}
	caller.orbitalScaleDetected = true
	caller.orbitalScaleDetectAt = 8
	caller.supercruiseHUDStates = []string{"ACTIVE", "ACTIVE"}
	inputs := map[string]any{
		"targetName": "LTT 11244 A 2", "targetLocked": true,
		"normalSpaceConfirmed": false, "supercruiseConfirmed": true,
		"autoThrottleConfirmed": true, "destinationMode": "ORBIT_HANDOFF",
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), inputs, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "NEAR_ORBIT_SAFETY_TRIGGERED") {
		t.Fatalf("error=%v", err)
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
	if caller.humanTakeoverCalls != 1 {
		t.Fatalf("human takeover calls=%d", caller.humanTakeoverCalls)
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

func TestEliteSupercruiseAssistClearsLineOfSightThenReacquiresGameOwnership(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
		"SUPERCRUISE", "SUPERCRUISE",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	caller.speedStates = []string{"MOVING", "MOVING", "MOVING", "MOVING", "STOPPED", "STOPPED", "STOPPED"}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) ||
		!contains(string(output), `"lineOfSightRecoveryCount":1`) ||
		!contains(string(output), `"agentFlightInputAfterAssistActive":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.lineOfSightCalls != 1 || caller.alignmentCalls != 2 || caller.visibleAlignmentCalls != 2 {
		t.Fatalf("lineOfSight=%d compass=%d visible=%d", caller.lineOfSightCalls, caller.alignmentCalls, caller.visibleAlignmentCalls)
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
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"CLEARING_LINE_OF_SIGHT"`) ||
		!contains(joined, `"phase":"REALIGNING_COMPASS_AFTER_SEPARATION"`) ||
		!contains(joined, `"phase":"COMPASS_HANDOFF_CONFIRMED"`) ||
		!contains(joined, `"phase":"REALIGNING_VISIBLE_TARGET_AFTER_SEPARATION"`) ||
		!contains(joined, `"phase":"REALIGNING_AFTER_LINE_OF_SIGHT"`) ||
		!contains(joined, `"phase":"REACQUIRING_ASSIST"`) {
		t.Fatalf("events=%s", joined)
	}
	ordered := []string{"CLEARING_LINE_OF_SIGHT", "REALIGNING_COMPASS_AFTER_SEPARATION", "COMPASS_HANDOFF_CONFIRMED", "REALIGNING_VISIBLE_TARGET_AFTER_SEPARATION", "REALIGNING_AFTER_LINE_OF_SIGHT", "REACQUIRING_ASSIST"}
	last := -1
	for _, phase := range ordered {
		index := strings.Index(joined, `"phase":"`+phase+`"`)
		if index <= last {
			t.Fatalf("phase order=%v events=%s", ordered, joined)
		}
		last = index
	}
}

func TestEliteSupercruiseAssistDoesNotRequestBlueZoneWhenRestoredFrameRequiresLineOfSight(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.postPanelStates = []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"}
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
		"SUPERCRUISE", "SUPERCRUISE",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.lineOfSightCalls != 1 {
		t.Fatalf("line-of-sight calls=%d", caller.lineOfSightCalls)
	}
	wantThrottles := []int{0, 100, 0, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"CLOSING_PANEL"`) ||
		!contains(joined, `"flightStatusSource":"CURRENT_FRAME"`) ||
		!contains(joined, `"lineOfSightRequiredConfirmations":1`) ||
		!contains(joined, `"phase":"CLEARING_LINE_OF_SIGHT"`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteSupercruiseAssistRestoredLineOfSightCandidateStillRequiresSecondFrame(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.postPanelStates = []string{"SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED"}
	caller.flightStates = []string{
		"FSD_CHARGING", "SUPERCRUISE",
		"UNKNOWN", "SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.lineOfSightCalls != 0 {
		t.Fatalf("single restored LOS frame entered recovery: calls=%d", caller.lineOfSightCalls)
	}
	wantThrottles := []int{0, 100, 0, 75}
	if len(caller.throttles) != len(wantThrottles) {
		t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
	}
	for index := range wantThrottles {
		if caller.throttles[index] != wantThrottles[index] {
			t.Fatalf("throttles=%v want=%v", caller.throttles, wantThrottles)
		}
	}
}

func TestEliteSupercruiseAssistEntryUnknownIsBoundedAndStopsShip(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = make([]string, 45)
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
	if caller.flightIndex != 45 {
		t.Fatalf("flight observations=%d", caller.flightIndex)
	}
	if len(caller.throttles) != 3 || caller.throttles[0] != 0 || caller.throttles[1] != 100 || caller.throttles[2] != 0 {
		t.Fatalf("failure compensation did not bound movement: throttles=%v", caller.throttles)
	}
}

func TestEliteSupercruiseAssistEntryAllowsLateT9CountdownThenRequiresHUD(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.flightStates = make([]string, 0, 40)
	for index := 0; index < 30; index++ {
		caller.flightStates = append(caller.flightStates, "FSD_CHARGING")
	}
	caller.flightStates = append(caller.flightStates,
		"UNKNOWN", "UNKNOWN",
		"SUPERCRUISE_ASSIST_ACTIVE", "SUPERCRUISE_ASSIST_ACTIVE",
		"UNKNOWN", "UNKNOWN", "UNKNOWN",
	)
	caller.supercruiseHUDStates = []string{"ACTIVE", "ACTIVE"}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"completed":true`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	if caller.flightIndex <= 30 || caller.supercruiseHUDIndex != 2 {
		t.Fatalf("flight observations=%d HUD observations=%d", caller.flightIndex, caller.supercruiseHUDIndex)
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

func TestEliteSupercruiseAssistNearOrbitStopsBeforeHumanTakeover(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.orbitalScaleDetected = true
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err == nil || !contains(err.Error(), "NEAR_ORBIT_SAFETY_TRIGGERED") {
		t.Fatalf("error=%v", err)
	}
	if caller.humanTakeoverCalls != 1 {
		t.Fatalf("human takeover calls=%d", caller.humanTakeoverCalls)
	}
	if len(caller.throttles) < 2 || caller.throttles[0] != 0 || caller.throttles[1] != 0 {
		t.Fatalf("zero-throttle ordering missing: %v", caller.throttles)
	}
	if caller.alignmentCalls != 0 || caller.visibleAlignmentCalls != 0 || caller.supercruiseKeys != 0 {
		t.Fatalf("flight continued after near-orbit Gate: compass=%d visible=%d fsd=%d", caller.alignmentCalls, caller.visibleAlignmentCalls, caller.supercruiseKeys)
	}
	joined := joinEventPhases(reporter.payloads)
	zeroIndex := strings.Index(joined, `"phase":"NEAR_ORBIT_SAFETY_TRIGGERED"`)
	handoffIndex := strings.Index(joined, `"phase":"HUMAN_TAKEOVER"`)
	if zeroIndex < 0 || handoffIndex <= zeroIndex || !contains(joined, `"humanTakeoverReady":true`) {
		t.Fatalf("events=%s", joined)
	}
}

func TestEliteSupercruiseAssistNearOrbitRejectsIncompleteHumanTakeoverPostcondition(t *testing.T) {
	caller := successfulSupercruiseAssistCaller()
	caller.orbitalScaleDetected = true
	caller.humanTakeoverOutput = json.RawMessage(`{"schemaVersion":2,"task":"PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER","completed":true,"pauseMenuConfirmed":true,"exitFocused":true,"firstExitSelectSent":true,"exitDestinationMenuConfirmed":true,"exitToMainMenuFocused":true,"exitToMainMenuSelectSent":true,"mainMenuConfirmed":false,"openAttempts":1,"postExitSamples":7,"finalObservation":{"state":"MAIN_MENU","reason":"MAIN_MENU_EXACT_ANCHORS_CONFIRMED","focusFillRatio":0,"rawTexts":["CONTINUE","SOCIAL","OPTIONS"]}}`)
	reporter := &fixtureReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSupercruiseAssistToDestinationPackage(t), supercruiseAssistInputs(), caller, reporter,
	)
	if err == nil || !contains(err.Error(), "NEAR_ORBIT_SAFE_EXIT_POSTCONDITION_NOT_CONFIRMED") {
		t.Fatalf("error=%v", err)
	}
	joined := joinEventPhases(reporter.payloads)
	if contains(joined, `"phase":"HUMAN_TAKEOVER"`) || contains(joined, `"humanTakeoverReady":true`) {
		t.Fatalf("incomplete postcondition emitted successful handoff: %s", joined)
	}
}
