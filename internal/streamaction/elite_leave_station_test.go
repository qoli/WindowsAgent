package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type leaveStationCaller struct {
	cycle        int
	forceMassOff bool
	throttles    []int
}

func (c *leaveStationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/flight-prompt-text":
		c.cycle++
		return json.RawMessage(`{"text":"fixture","confidence":1}`), nil
	case "elite-dangerous/flight-status":
		state := "UNKNOWN"
		if c.cycle == 2 || c.cycle == 3 {
			state = "AUTO_LAUNCH"
		}
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}})
	case "elite-dangerous/ship-status":
		state := "ON"
		if c.forceMassOff || c.cycle >= 8 {
			state = "OFF"
		}
		return json.Marshal(map[string]any{"shipStatus": map[string]any{"massLock": map[string]any{"state": state}}})
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
		})
	default:
		return nil, errors.New("unexpected Action: " + id)
	}
}

type leaveStationReporter struct {
	phases []string
}

func (r *leaveStationReporter) Emit(_ context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	if eventType != "action.leave-station.update" {
		return eventstream.Event{}, errors.New("unexpected event type")
	}
	var value struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return eventstream.Event{}, err
	}
	r.phases = append(r.phases, value.Phase)
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
	if string(output) != `{"completed":true,"finalMassLock":"OFF","finalPhase":"COMPLETED","sampleCount":9,"schemaVersion":1,"task":"LEAVE_STATION"}` {
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
