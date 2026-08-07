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

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type leaveStationCaller struct {
	cycle                    int
	forceMassOff             bool
	massOffAt                int
	speedAlwaysUnknown       bool
	promptGarbageAfterLaunch bool
	invalidUnknownSpeedValue bool
	autoLaunchCycles         map[int]bool
	speedByCycle             map[int]int
	throttles                []int
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
		state := "UNKNOWN"
		var value any
		if speed, ok := c.visibleSpeed(); !c.speedAlwaysUnknown && ok {
			state = "KNOWN"
			value = speed
		} else if c.invalidUnknownSpeedValue {
			value = 99
		}
		rawText := any(nil)
		detectionConfidence := 0.0
		recognitionConfidence := 0.0
		reason := "SPEED_BOX_NOT_FOUND"
		if state == "KNOWN" {
			rawText = strconv.Itoa(value.(int))
			detectionConfidence = 0.91
			recognitionConfidence = 0.88
			reason = "VISUAL_SPEED_CONFIRMED"
		}
		return json.Marshal(map[string]any{"speed": map[string]any{
			"state": state, "displayValue": value,
			"evidence": map[string]any{
				"reason": reason, "rawText": rawText,
				"detectionConfidence":   detectionConfidence,
				"recognitionConfidence": recognitionConfidence,
			},
		}})
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
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
	phases   []string
	payloads []map[string]any
}

func (r *leaveStationReporter) Emit(_ context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	if eventType != "action.leave-station.update" {
		return eventstream.Event{}, errors.New("unexpected event type")
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return eventstream.Event{}, err
	}
	r.phases = append(r.phases, value["phase"].(string))
	r.payloads = append(r.payloads, value)
	return eventstream.Event{Sequence: uint64(len(r.phases))}, nil
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
	if string(output) != `{"completed":true,"finalCommandedThrottle":0,"finalMassLock":"OFF","finalPhase":"COMPLETED","lastObservedSpeedDisplayValue":100,"lastObservedSpeedState":"KNOWN","sampleCount":15,"schemaVersion":2,"task":"LEAVE_STATION"}` {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 100 || caller.throttles[1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	joined := strings.Join(reporter.phases, ",")
	if !strings.HasPrefix(joined, "AWAITING_AUTO_LAUNCH,AWAITING_AUTO_LAUNCH") ||
		!strings.Contains(joined, "AUTO_LAUNCH_ACTIVE") ||
		!strings.Contains(joined, "DEPARTING") || !strings.HasSuffix(joined, "COMPLETED") {
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
	if completed["commandedThrottle"] != float64(0) || completed["observedSpeedDisplayValue"] != float64(100) ||
		completed["gateDecision"] != "MASS_LOCK_RELEASE_CONFIRMED" {
		t.Fatalf("completed payload=%#v", completed)
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
