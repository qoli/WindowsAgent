package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type multiSystemTransitCaller struct {
	statusIndex             int
	planCalls               int
	changeRouteAtPlan       int
	jumpTargets             []string
	jumpModes               []string
	clearanceTargets        []string
	throttles               []int64
	fuelMain                float64
	resumeAtSecond          bool
	missingDestination      bool
	lockAcquired            bool
	routeTargetControls     int
	journalPositionEvent    string
	routeTargetNeedsRefresh bool
	routeContextRefreshed   bool
	plotRouteCalls          int
	executedJumps           int
}

func (c *multiSystemTransitCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/filesystem/nav-route":
		return json.RawMessage(`{"schemaVersion":1,"state":"AVAILABLE","freshness":"UNKNOWN","source":{"sourceTimestamp":"2026-08-10T08:00:00Z"},"data":{"event":"NavRoute"}}`), nil
	case "elite-dangerous/nav-route-plan":
		c.planCalls++
		routeID := "2026-08-10T08:00:00Z:3:2"
		if c.changeRouteAtPlan != 0 && c.planCalls >= c.changeRouteAtPlan {
			routeID = "2026-08-10T08:01:00Z:3:2"
		}
		return json.Marshal(map[string]any{
			"state": "READY", "routeId": routeID, "freshness": "UNKNOWN", "jumpCount": int64(2),
			"origin": map[string]any{"name": "Origin", "systemAddress": int64(1)},
			"hops": []any{
				map[string]any{"index": int64(1), "name": "Middle", "systemAddress": int64(2)},
				map[string]any{"index": int64(2), "name": "Destination", "systemAddress": int64(3)},
			},
		})
	case "elite-dangerous/filesystem/status":
		c.statusIndex++
		fuelMain := c.fuelMain
		if fuelMain == 0 {
			fuelMain = 20.0 - float64(c.statusIndex)
		}
		freshness := "CURRENT"
		if c.statusIndex == 1 {
			freshness = "STALE"
		}
		destinationName := "Middle"
		destinationAddress := int64(2)
		if c.executedJumps >= 1 || c.resumeAtSecond {
			destinationName = "Destination"
			destinationAddress = 3
		}
		data := map[string]any{
			"Fuel": map[string]any{"FuelMain": fuelMain},
		}
		if !c.missingDestination || c.lockAcquired {
			data["Destination"] = map[string]any{"Name": destinationName, "System": destinationAddress}
		}
		return json.Marshal(map[string]any{
			"state": "AVAILABLE", "freshness": freshness,
			"source": map[string]any{"sourceTimestamp": "2026-08-10T08:00:0" + string(rune('0'+c.statusIndex)) + "Z"},
			"data":   data,
		})
	case "elite-dangerous/filesystem/journal-navigation-tail":
		event := c.journalPositionEvent
		if event == "" {
			event = "FSDJump"
		}
		return json.Marshal(map[string]any{"events": []any{map[string]any{
			"event": event, "timestamp": "2026-08-10T07:59:00Z", "StarSystem": "Origin", "SystemAddress": int64(1),
		}}})
	case "elite-dangerous/ui-control":
		if inputs["control"] != "TARGET_NEXT_ROUTE_SYSTEM" {
			return nil, errors.New("unexpected UI control")
		}
		c.routeTargetControls++
		if !c.routeTargetNeedsRefresh || c.routeContextRefreshed {
			c.lockAcquired = true
		}
		return json.RawMessage(`{"selection":"TARGET_NEXT_ROUTE_SYSTEM","control":"TargetNextRouteSystem"}`), nil
	case "elite-dangerous/plot-route-to-system":
		if inputs["targetSystem"] != "Destination" || inputs["maxJumps"] != int64(4) || inputs["refreshExistingContext"] != true {
			return nil, errors.New("unexpected route-context refresh input")
		}
		c.plotRouteCalls++
		c.routeContextRefreshed = true
		return json.RawMessage(`{"result":"REFRESHED","routeId":"2026-08-10T08:00:00Z:3:2"}`), nil
	case "elite-dangerous/hyperspace-jump-to-system":
		c.jumpTargets = append(c.jumpTargets, inputs["targetSystem"].(string))
		c.jumpModes = append(c.jumpModes, inputs["startMode"].(string))
		c.executedJumps++
		return json.RawMessage(`{"completed":true,"finalPhase":"ARRIVED_IN_SUPERCRUISE","arrivalBrakeSent":true}`), nil
	case "elite-dangerous/clear-hyperspace-occlusion":
		c.clearanceTargets = append(c.clearanceTargets, inputs["targetName"].(string))
		if inputs["startMode"] != "SUPERCRUISE" {
			return nil, errors.New("arrival clearance did not use SUPERCRUISE mode")
		}
		return json.RawMessage(`{"completed":true,"fixedOutwardTurnCompleted":true,"fixedTurnDurationMs":6400,"finalSupercruiseConfirmed":true,"finalCommandedThrottle":0,"entryAlignmentEvidence":"EXISTING_SUPERCRUISE_FIXED_SPHERE_SEPARATION","supercruiseEscapeDurationMs":30000}`), nil
	case "elite-dangerous/set-throttle":
		percent, _ := inputs["percent"].(int64)
		c.throttles = append(c.throttles, percent)
		return json.RawMessage(`{"control":"SetSpeedZero"}`), nil
	case "elite-dangerous/leave-station":
		return json.RawMessage(`{"completed":true}`), nil
	default:
		return nil, errors.New("unexpected multi-System child Action: " + id)
	}
}

func TestEliteMultiSystemTransitRestoresMissingRouteTargetFromJournalPosition(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{missingDestination: true, fuelMain: 64}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.routeTargetControls != 1 {
		t.Fatalf("routeTargetControls=%d", caller.routeTargetControls)
	}
	if !contains(string(output), `"completedJumps":2`) || !contains(joinEventPhases(reporter.payloads), "RESTORING_ROUTE_TARGET") {
		t.Fatalf("output=%s events=%s", output, joinEventPhases(reporter.payloads))
	}
}

func TestEliteMultiSystemTransitRestoresMissingRouteTargetFromLoginLocation(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{missingDestination: true, journalPositionEvent: "Location", fuelMain: 64}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.routeTargetControls != 1 || !contains(string(output), `"completedJumps":2`) || !contains(joinEventPhases(reporter.payloads), "RESTORING_ROUTE_TARGET") {
		t.Fatalf("routeTargetControls=%d output=%s events=%s", caller.routeTargetControls, output, joinEventPhases(reporter.payloads))
	}
}

func TestEliteMultiSystemTransitRefreshesGalaxyMapContextWhenFirstRouteTargetRequestIsRejected(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{
		missingDestination: true, journalPositionEvent: "Location", routeTargetNeedsRefresh: true, fuelMain: 64,
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.routeTargetControls != 2 || caller.plotRouteCalls != 1 || !contains(string(output), `"completedJumps":2`) {
		t.Fatalf("routeTargetControls=%d plotRouteCalls=%d output=%s", caller.routeTargetControls, caller.plotRouteCalls, output)
	}
	phases := joinEventPhases(reporter.payloads)
	if !contains(phases, "REFRESHING_ROUTE_CONTEXT") || !contains(phases, "RESTORING_ROUTE_TARGET") {
		t.Fatalf("events=%s", phases)
	}
}

func TestEliteMultiSystemTransitResumesAtStatusDestinationHop(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{resumeAtSecond: true}
	inputs := multiSystemInputs()
	inputs["startMode"] = "SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.jumpTargets) != 1 || caller.jumpTargets[0] != "Destination" ||
		!contains(string(output), `"completedJumps":2`) || !contains(joinEventPhases(reporter.payloads), "ROUTE_RESUMED") {
		t.Fatalf("jumpTargets=%v output=%s events=%s", caller.jumpTargets, output, joinEventPhases(reporter.payloads))
	}
}

func multiSystemPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "multi-system-transit"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func multiSystemInputs() map[string]any {
	return map[string]any{
		"destinationSystem":    "Destination",
		"startMode":            "NORMAL_SPACE",
		"normalSpaceConfirmed": true,
		"supercruiseConfirmed": false,
		"maxJumps":             int64(4),
		"routeFuelConfirmed":   true,
		"minimumFuelMain":      2.0,
	}
}

func TestEliteMultiSystemTransitConsumesFrozenRouteInOrder(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"FINAL_SYSTEM_REACHED"`) || !contains(string(output), `"completedJumps":2`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.jumpTargets) != 2 || caller.jumpTargets[0] != "Middle" || caller.jumpTargets[1] != "Destination" {
		t.Fatalf("jumpTargets=%v", caller.jumpTargets)
	}
	if len(caller.jumpModes) != 2 || caller.jumpModes[0] != "NORMAL_SPACE" || caller.jumpModes[1] != "SUPERCRUISE" {
		t.Fatalf("jumpModes=%v", caller.jumpModes)
	}
	if len(caller.clearanceTargets) != 1 || caller.clearanceTargets[0] != "Destination" {
		t.Fatalf("clearanceTargets=%v", caller.clearanceTargets)
	}
	if caller.planCalls != 2 || len(caller.throttles) != 0 {
		t.Fatalf("planCalls=%d throttles=%v", caller.planCalls, caller.throttles)
	}
	joined := joinEventPhases(reporter.payloads)
	for _, phase := range []string{"ROUTE_READY", "HOP_STARTING", "HOP_COMPLETED", "ARRIVAL_CLEARANCE", "ARRIVAL_CLEARANCE_COMPLETED", "ROUTE_REVALIDATED", "FINAL_SYSTEM_REACHED"} {
		if !contains(joined, phase) {
			t.Fatalf("missing phase %s in %s", phase, joined)
		}
	}
}

func TestEliteMultiSystemTransitFailsIfRouteIdentityChangesAndStops(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{changeRouteAtPlan: 2}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "NavRoute identity changed during multi-System transit") {
		t.Fatalf("error=%v", err)
	}
	if !equalInt64s(caller.throttles, []int64{0}) || len(caller.jumpTargets) != 1 {
		t.Fatalf("throttles=%v jumpTargets=%v", caller.throttles, caller.jumpTargets)
	}
}

func TestEliteMultiSystemTransitRejectsInsufficientFuelBeforeInput(t *testing.T) {
	pkg, err := Load(multiSystemPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &multiSystemTransitCaller{fuelMain: 1.0}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, multiSystemInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FuelMain is below minimumFuelMain") {
		t.Fatalf("error=%v", err)
	}
	if !equalInt64s(caller.throttles, []int64{0}) || len(caller.jumpTargets) != 0 {
		t.Fatalf("throttles=%v jumpTargets=%v", caller.throttles, caller.jumpTargets)
	}
}
