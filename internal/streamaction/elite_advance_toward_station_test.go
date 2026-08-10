package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type advanceTowardStationCaller struct {
	observations []json.RawMessage
	index        int
	throttles    []int
}

func (c *advanceTowardStationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/request-docking-range":
		if c.index >= len(c.observations) {
			return nil, errors.New("unexpected Station distance observation")
		}
		result := c.observations[c.index]
		c.index++
		return result, nil
	case "elite-dangerous/set-throttle":
		percent, ok := inputs["percent"].(int64)
		if !ok {
			return nil, errors.New("throttle percent is not an integer")
		}
		c.throttles = append(c.throttles, int(percent))
		control := map[int64]string{0: "SetSpeedZero", 75: "SetSpeed75", 100: "SetSpeed100"}[percent]
		return json.Marshal(map[string]any{"schemaVersion": 1, "selection": percent, "control": control})
	default:
		return nil, errors.New("unexpected advance-toward-station child Action: " + id)
	}
}

func stationDistanceObservation(state string, distance any, reason string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"requestDockingRange": map[string]any{
			"state": state, "distanceMeters": distance,
			"evidence": map[string]any{"reason": reason},
		},
	})
	return value
}

func loadEliteAdvanceTowardStationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "advance-toward-station"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func advanceTowardStationInputs() map[string]any {
	return map[string]any{
		"throttlePercent":             float64(75),
		"stopAtStationDistanceMeters": float64(7500 - 1),
		"maxDurationMs":               float64(10000),
	}
}

func TestEliteAdvanceTowardStationStopsBeforeConfirmingTargetDistance(t *testing.T) {
	caller := &advanceTowardStationCaller{observations: []json.RawMessage{
		stationDistanceObservation("DENIED", 9000, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("DENIED", 8900, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("DENIED", 8200, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("ALLOWED", 7400, "DISPLAY_DISTANCE_BELOW_THRESHOLD"),
		stationDistanceObservation("ALLOWED", 7350, "DISPLAY_DISTANCE_BELOW_THRESHOLD"),
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAdvanceTowardStationPackage(t), advanceTowardStationInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"STATION_DISTANCE_REACHED"`) ||
		!contains(string(output), `"initialStationDistanceMeters":8900`) ||
		!contains(string(output), `"finalStationDistanceMeters":7350`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 75 || caller.throttles[1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	targetStopSeen := false
	for _, payload := range reporter.payloads {
		if contains(string(payload), `"reason":"TARGET_DISTANCE_CANDIDATE_STOPPED"`) &&
			contains(string(payload), `"commandedThrottle":0`) {
			targetStopSeen = true
		}
	}
	if !targetStopSeen {
		t.Fatalf("payloads=%v", reporter.payloads)
	}
}

func TestEliteAdvanceTowardStationDoesNotMoveWhenTargetAlreadyReached(t *testing.T) {
	caller := &advanceTowardStationCaller{observations: []json.RawMessage{
		stationDistanceObservation("ALLOWED", 7100, "DISPLAY_DISTANCE_BELOW_THRESHOLD"),
		stationDistanceObservation("ALLOWED", 7000, "DISPLAY_DISTANCE_BELOW_THRESHOLD"),
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAdvanceTowardStationPackage(t), advanceTowardStationInputs(), caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"TARGET_ALREADY_REACHED"`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.throttles) != 1 || caller.throttles[0] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}

func TestEliteAdvanceTowardStationStopsWhenTrustedDistanceMovesAway(t *testing.T) {
	caller := &advanceTowardStationCaller{observations: []json.RawMessage{
		stationDistanceObservation("DENIED", 9000, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("DENIED", 8950, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("DENIED", 9000, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
		stationDistanceObservation("DENIED", 9100, "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAdvanceTowardStationPackage(t), advanceTowardStationInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "increased for two consecutive trusted samples") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 2 || caller.throttles[0] != 75 || caller.throttles[1] != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}

func TestEliteAdvanceTowardStationNeverMovesWithoutStableDistanceBaseline(t *testing.T) {
	caller := &advanceTowardStationCaller{observations: []json.RawMessage{
		stationDistanceObservation("UNKNOWN", nil, "DISTANCE_TEXT_INVALID"),
		stationDistanceObservation("UNKNOWN", nil, "DISTANCE_REGIONS_MISSING"),
		stationDistanceObservation("UNKNOWN", nil, "DISTANCE_TEXT_INVALID"),
		stationDistanceObservation("UNKNOWN", nil, "DISTANCE_REGIONS_MISSING"),
		stationDistanceObservation("UNKNOWN", nil, "DISTANCE_TEXT_INVALID"),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteAdvanceTowardStationPackage(t), advanceTowardStationInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !strings.Contains(err.Error(), "no throttle was applied") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.throttles) != 0 {
		t.Fatalf("throttles=%v", caller.throttles)
	}
}
