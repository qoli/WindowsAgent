package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type alignStationTargetCaller struct {
	observations []json.RawMessage
	index        int
	throttles    []int
	controls     []string
}

func (c *alignStationTargetCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		return json.RawMessage(`{"schemaVersion":1,"selection":"0","control":"SetSpeedZero"}`), nil
	case "elite-dangerous/compass":
		if c.index >= len(c.observations) {
			return nil, errors.New("unexpected Compass observation")
		}
		result := c.observations[c.index]
		c.index++
		return result, nil
	case "elite-dangerous/ship-attitude-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("attitude control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.Marshal(map[string]any{"schemaVersion": 1, "selection": control, "control": control})
	default:
		return nil, errors.New("unexpected align-station-target child Action: " + id)
	}
}

func alignObservation(presentation string, offsetX, offsetY int, distance float64, inside bool) json.RawMessage {
	hemisphere := "FRONT"
	if presentation == "HOLLOW" {
		hemisphere = "REAR"
	}
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 3,
		"target": map[string]any{
			"detected": true, "presentation": presentation, "hemisphere": hemisphere,
			"offsetX": offsetX, "offsetY": offsetY, "centerDistancePixels": distance,
			"centerZone": map[string]any{"inside": inside},
		},
	})
	return value
}

func loadEliteAlignStationTargetPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "align-station-target"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteAlignStationTargetTurnsRearMarkerThenStablyCenters(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("HOLLOW", 0, 0, 0, true),
		alignObservation("HOLLOW", -10, 0, 10, false),
		alignObservation("SOLID", 8, 0, 8, false),
		alignObservation("SOLID", 3, 0, 3, true),
		alignObservation("SOLID", 2, 0, 2, true),
		alignObservation("SOLID", 1, 0, 1, true),
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"commandCount":3,"completed":true,"finalObservation":{"schemaVersion":3,"target":{"centerDistancePixels":1,"centerZone":{"inside":true},"detected":true,"hemisphere":"FRONT","offsetX":1,"offsetY":0,"presentation":"SOLID"}},"finalPhase":"COMPLETED","sampleCount":6,"schemaVersion":1,"stableConfirmations":3,"task":"ALIGN_STATION_TARGET"}` {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	wantControls := []string{"YAW_RIGHT", "YAW_RIGHT", "YAW_RIGHT"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v", caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
}

func TestEliteAlignStationTargetUsesDominantFrontAxis(t *testing.T) {
	caller := &alignStationTargetCaller{observations: []json.RawMessage{
		alignObservation("SOLID", -20, 4, 20.396, false),
		alignObservation("SOLID", 2, 8, 8.246, false),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
		alignObservation("SOLID", 0, 0, 0, true),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"YAW_LEFT", "PITCH_DOWN"}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
}

func TestEliteAlignStationTargetFailsBeforeInputWhenTargetMissing(t *testing.T) {
	missing := json.RawMessage(`{"schemaVersion":3,"target":{"detected":false,"presentation":"UNKNOWN","hemisphere":"UNKNOWN","offsetX":null,"offsetY":null,"centerDistancePixels":null,"centerZone":{"inside":null}}}`)
	caller := &alignStationTargetCaller{observations: []json.RawMessage{missing}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAlignStationTargetPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "establish the intended Station target lock") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("controls=%v", caller.controls)
	}
}
