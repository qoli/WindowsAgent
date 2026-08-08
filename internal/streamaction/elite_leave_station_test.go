package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type leaveStationCaller struct {
	cycle                       int
	forceMassOff                bool
	massOffAt                   int
	speedAlwaysUnknown          bool
	promptGarbageAfterLaunch    bool
	invalidUnknownSpeedValue    bool
	temporalLowSpeedFrom        int
	temporalLowSpeedConfidence  float64
	temporalLowSpeedMargin      float64
	temporalLowSpeedAlternates  bool
	temporalLowSpeedRawMismatch bool
	stopEvidenceNever           bool
	wgcFailuresAfterThrottle100 int
	throttleZeroCommanded       bool
	flightPromptCalls           int
	shipStatusCalls             int
	shipSpeedCalls              int
	autoLaunchCycles            map[int]bool
	speedByCycle                map[int]int
	throttles                   []int
}

func (c *leaveStationCaller) isAutoLaunchCycle() bool {
	if c.autoLaunchCycles != nil {
		return c.autoLaunchCycles[c.cycle]
	}
	return c.cycle >= 2 && c.cycle <= 4
}

func (c *leaveStationCaller) visibleSpeed() (int, bool) {
	if c.speedByCycle != nil {
		value, ok := c.speedByCycle[c.cycle]
		return value, ok
	}
	values := map[int]int{3: 20, 4: 55, 9: 7, 11: 7, 12: 40, 13: 80, 14: 100, 15: 100}
	value, ok := values[c.cycle]
	return value, ok
}

func (c *leaveStationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/flight-prompt-text":
		c.flightPromptCalls++
		c.cycle++
		text := ""
		confidence := 0.0
		if c.isAutoLaunchCycle() {
			text = "AUTO LAUNCH IN PROGRESS"
			confidence = 0.99
		} else if c.promptGarbageAfterLaunch && c.cycle >= 4 {
			text = "V0AVVM"
			confidence = 0.43
		}
		return json.Marshal(map[string]any{"text": text, "confidence": confidence})
	case "elite-dangerous/flight-status":
		state := "UNKNOWN"
		if c.isAutoLaunchCycle() {
			state = "AUTO_LAUNCH"
		}
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}})
	case "elite-dangerous/ship-status":
		c.shipStatusCalls++
		state := "ON"
		massOffAt := c.massOffAt
		if massOffAt == 0 {
			massOffAt = 14
		}
		if c.forceMassOff || c.cycle >= massOffAt {
			state = "OFF"
		}
		return json.Marshal(map[string]any{"shipStatus": map[string]any{"massLock": map[string]any{"state": state}}})
	case "elite-dangerous/ship-speed":
		c.shipSpeedCalls++
		if len(c.throttles) > 0 && c.throttles[len(c.throttles)-1] == 100 && c.wgcFailuresAfterThrottle100 > 0 {
			c.wgcFailuresAfterThrottle100--
			return nil, capture.Failure("capture_readback_failed", "failed to create the region unordered-access view", errors.New("HRESULT 0x80070057"))
		}
		if c.throttleZeroCommanded {
			if c.stopEvidenceNever {
				return json.Marshal(map[string]any{"speed": map[string]any{
					"state": "UNKNOWN", "displayValue": nil,
					"evidence": map[string]any{
						"reason": "CONSTRAINED_CONFIDENCE_LOW", "rawText": "0",
						"rawConfidence": 0.44, "constrainedText": "0",
						"constrainedConfidence": 0.44, "rawConstraintMargin": 0.0,
					},
				}})
			}
			return json.Marshal(map[string]any{"speed": map[string]any{
				"state": "UNKNOWN", "displayValue": nil,
				"evidence": map[string]any{
					"reason": "CONSTRAINED_CONFIDENCE_LOW", "rawText": "0",
					"rawConfidence": 0.49, "constrainedText": "0",
					"constrainedConfidence": 0.49, "rawConstraintMargin": 0.0,
				},
			}})
		}
		if c.temporalLowSpeedFrom > 0 && c.cycle >= c.temporalLowSpeedFrom {
			text := "7"
			if c.temporalLowSpeedAlternates && c.cycle%2 == 0 {
				text = "8"
			}
			rawText := text
			if c.temporalLowSpeedRawMismatch {
				rawText = "1"
			}
			return json.Marshal(map[string]any{"speed": map[string]any{
				"state": "UNKNOWN", "displayValue": nil,
				"evidence": map[string]any{
					"reason": "CONSTRAINED_CONFIDENCE_LOW", "rawText": rawText,
					"rawConfidence": c.temporalLowSpeedConfidence, "constrainedText": text,
					"constrainedConfidence": c.temporalLowSpeedConfidence,
					"rawConstraintMargin":   c.temporalLowSpeedMargin,
				},
			}})
		}
		state := "UNKNOWN"
		var value any
		if speed, ok := c.visibleSpeed(); !c.speedAlwaysUnknown && ok {
			state = "KNOWN"
			value = speed
		} else if c.invalidUnknownSpeedValue {
			value = 99
		}
		rawText := any(nil)
		constrainedText := any(nil)
		rawConfidence := 0.0
		constrainedConfidence := 0.0
		margin := 0.0
		reason := "SPEED_BOX_NOT_FOUND"
		if state == "KNOWN" {
			rawText = strconv.Itoa(value.(int))
			constrainedText = rawText
			rawConfidence = 0.88
			constrainedConfidence = 0.88
			reason = "VISUAL_SPEED_CONFIRMED"
		}
		return json.Marshal(map[string]any{"speed": map[string]any{
			"state": state, "displayValue": value,
			"evidence": map[string]any{
				"reason": reason, "rawText": rawText,
				"rawConfidence": rawConfidence, "constrainedText": constrainedText,
				"constrainedConfidence": constrainedConfidence, "rawConstraintMargin": margin,
			},
		}})
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		if percent == 0 {
			c.throttleZeroCommanded = true
		}
		control := "SetSpeedZero"
		key := "Key_X"
		selection := "0"
		if percent == 100 {
			control = "SetSpeed100"
			key = "Key_F7"
			selection = "100"
		}
		return json.Marshal(map[string]any{
			"schemaVersion": 1, "selection": selection, "control": control,
			"key": key, "activePreset": "ControlPadKeyboard", "bindingFile": "ControlPadKeyboard.4.2.binds",
			"bindingSource": "frontier-active-preset-v1", "backend": "sendinput-scancode",
			"scanCode": 31, "extended": false, "holdMs": 40,
		})
	default:
		return nil, errors.New("unexpected Action: " + id)
	}
}

type leaveStationReporter struct {
	phases     []string
	payloads   []map[string]any
	activities []string
	events     uint64
}

func (r *leaveStationReporter) Emit(_ context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	r.events++
	if eventType == ActivityEventType {
		var activity struct {
			Message string `json:"message"`
			Level   string `json:"level"`
		}
		if err := json.Unmarshal(payload, &activity); err != nil {
			return eventstream.Event{}, err
		}
		r.activities = append(r.activities, activity.Message)
		return eventstream.Event{Sequence: r.events}, nil
	}
	if eventType != "action.leave-station.update" {
		return eventstream.Event{}, errors.New("unexpected event type")
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return eventstream.Event{}, err
	}
	r.phases = append(r.phases, value["phase"].(string))
	r.payloads = append(r.payloads, value)
	return eventstream.Event{Sequence: r.events}, nil
}

func TestEliteLeaveStationWorkflowWaitsForModelThenControlsThrottle(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{}
	reporter := &leaveStationReporter{}
	runner := Runner{Sleep: immediateSleep}
	output, err := runner.Run(context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"completed":true,"finalCommandedThrottle":0,"finalMassLock":"OFF","finalPhase":"COMPLETED","finalStopState":"CONFIRMED","lastObservedSpeedConstrainedConfidence":0.49,"lastObservedSpeedConstrainedText":"0","lastObservedSpeedDisplayValue":null,"lastObservedSpeedRawConstraintMargin":0,"lastObservedSpeedRawText":"0","lastObservedSpeedState":"UNKNOWN","sampleCount":18,"schemaVersion":3,"task":"LEAVE_STATION","zeroSpeedConfirmations":3}` {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	if caller.flightPromptCalls != 15 || caller.shipStatusCalls != 15 || caller.shipSpeedCalls != 18 {
		t.Fatalf("unexpected observation calls: prompt=%d status=%d speed=%d", caller.flightPromptCalls, caller.shipStatusCalls, caller.shipSpeedCalls)
	}
	joined := strings.Join(reporter.phases, ",")
	if !strings.HasPrefix(joined, "AWAITING_AUTO_LAUNCH,AWAITING_AUTO_LAUNCH") ||
		!strings.Contains(joined, "AUTO_LAUNCH_ACTIVE") ||
		!strings.Contains(joined, "DEPARTING") || !strings.Contains(joined, "VERIFYING_STOP") ||
		!strings.HasSuffix(joined, "COMPLETED") {
		t.Fatalf("phases=%v", reporter.phases)
	}
	if len(reporter.payloads) == 0 {
		t.Fatal("leave-station emitted no payloads")
	}
	var departing map[string]any
	for _, payload := range reporter.payloads {
		if payload["commandedThrottle"] == float64(100) && payload["throttleCommand"] != nil {
			departing = payload
			break
		}
	}
	if departing == nil {
		t.Fatal("leave-station emitted no throttle-100 command payload")
	}
	if departing["commandedThrottle"] != float64(100) || departing["observedSpeedState"] != "KNOWN" ||
		departing["observedSpeedDisplayValue"] != float64(7) || departing["gateDecision"] != "HANDOVER_CONFIRMED" {
		t.Fatalf("first departing payload=%#v", departing)
	}
	completed := reporter.payloads[len(reporter.payloads)-1]
	if completed["commandedThrottle"] != float64(0) || completed["observedSpeedState"] != "UNKNOWN" ||
		completed["observedSpeedDisplayValue"] != nil || completed["gateDecision"] != "MASS_LOCK_RELEASE_CONFIRMED" ||
		completed["stopGateDecision"] != "ZERO_SPEED_CONFIRMED" || completed["zeroSpeedConfirmations"] != float64(3) ||
		completed["observationScope"] != "SPEED_ONLY" || completed["massLock"] != "UNKNOWN" {
		t.Fatalf("completed payload=%#v", completed)
	}
}

func TestEliteLeaveStationWorkflowFailsWhenZeroSpeedIsNotVisuallyConfirmed(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{stopEvidenceNever: true}
	reporter := &leaveStationReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, reporter,
	)
	if err == nil || !strings.Contains(err.Error(), "Zero speed was not visually confirmed after throttle 0") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttle controls=%v", caller.throttles)
	}
	if strings.Contains(strings.Join(reporter.phases, ","), "COMPLETED") {
		t.Fatalf("unexpected completed phase=%v", reporter.phases)
	}
}

func TestEliteLeaveStationWorkflowSkipsFiveWGCErrorsAfterThrottle100(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{massOffAt: 20, wgcFailuresAfterThrottle100: 5}
	reporter := &leaveStationReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttle controls=%v", caller.throttles)
	}
	errorsSeen := 0
	for _, payload := range reporter.payloads {
		if payload["phase"] == "OBSERVATION_ERROR" {
			errorsSeen++
		}
	}
	if errorsSeen != 5 {
		t.Fatalf("observation errors=%d payloads=%v", errorsSeen, reporter.payloads)
	}
}

func TestEliteLeaveStationWorkflowSendsThrottleZeroWhenSixthWGCErrorFailsDeparture(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{massOffAt: 2000, wgcFailuresAfterThrottle100: 6}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "WGC observation error limit exceeded after five skipped errors") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("failure compensation throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowRejectsMassLockOffBeforeAutoLaunch(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{forceMassOff: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "Mass Lock became OFF before Auto Launch") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowDoesNotTreatUnknownSpeedAsAutoLaunchHandover(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{massOffAt: 2000, speedAlwaysUnknown: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "Auto Launch visual handover was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowAcceptsRepeatedQualifiedTemporalLowSpeedEvidence(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{
		temporalLowSpeedFrom:       9,
		temporalLowSpeedConfidence: 0.45,
	}
	reporter := &leaveStationReporter{}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttle controls=%v", caller.throttles)
	}
	var handover map[string]any
	for _, payload := range reporter.payloads {
		if payload["commandedThrottle"] == float64(100) && payload["throttleCommand"] != nil {
			handover = payload
			break
		}
	}
	if handover == nil || handover["observedSpeedState"] != "UNKNOWN" ||
		handover["observedSpeedConstrainedText"] != "7" ||
		handover["temporalLowSpeedConfirmations"] != float64(4) ||
		handover["handoverEvidence"] != "TEMPORAL_LOW_CONFIDENCE" ||
		handover["gateDecision"] != "HANDOVER_CONFIRMED" {
		t.Fatalf("temporal handover payload=%#v", handover)
	}
}

func TestEliteLeaveStationWorkflowRejectsTemporalLowSpeedBelowWorkflowThreshold(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{
		massOffAt:                  2000,
		temporalLowSpeedFrom:       9,
		temporalLowSpeedConfidence: 0.39,
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "Auto Launch visual handover was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowRejectsChangingTemporalLowSpeedText(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{
		massOffAt:                  2000,
		temporalLowSpeedFrom:       9,
		temporalLowSpeedConfidence: 0.45,
		temporalLowSpeedAlternates: true,
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "Auto Launch visual handover was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowAllowsUnclassifiedPromptAfterObservedLifecycle(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{promptGarbageAfterLaunch: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowIgnoresIsolatedLowSpeedWhileAutoLaunchVisible(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{
		massOffAt:        2000,
		autoLaunchCycles: map[int]bool{2: true, 3: true, 4: true},
		speedByCycle:     map[int]int{3: 20, 4: 0},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "Auto Launch visual handover was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func TestEliteLeaveStationWorkflowFailsOnInconsistentSpeedEvidence(t *testing.T) {
	pkg := loadEliteLeaveStationPackage(t)
	caller := &leaveStationCaller{invalidUnknownSpeedValue: true}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), pkg, map[string]any{"stationConfirmed": true}, caller, &leaveStationReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "ship-speed returned UNKNOWN with displayValue") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("unexpected throttle controls=%v", caller.throttles)
	}
}

func loadEliteLeaveStationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "leave-station"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func immediateSleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}
