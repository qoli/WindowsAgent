package streamaction

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func hyperspaceJumpPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "hyperspace-jump-to-system"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func hyperspaceJumpInputs() map[string]any {
	return map[string]any{
		"targetSystem":         "Acihaut",
		"targetLockConfirmed":  false,
		"startMode":            "NORMAL_SPACE",
		"normalSpaceConfirmed": true,
		"supercruiseConfirmed": false,
	}
}

func TestEliteHyperspaceJumpUsesCallerConfirmedTargetWithoutNavigation(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{hyperspaceStates: []string{
		"COCKPIT_PRESENT", "FSD_CHARGING", "COCKPIT_ABSENT", "COCKPIT_ABSENT", "COCKPIT_PRESENT", "COCKPIT_PRESENT",
	}}
	inputs := hyperspaceJumpInputs()
	inputs["targetLockConfirmed"] = true
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(context.Background(), pkg, inputs, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if caller.systemLocks != 0 {
		t.Fatalf("systemLocks=%d", caller.systemLocks)
	}
}

func TestEliteHyperspaceJumpBrakesOnFirstReturningCockpitFrame(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{hyperspaceStates: []string{
		"COCKPIT_PRESENT",
		"FSD_CHARGING", "COCKPIT_PRESENT", "COCKPIT_ABSENT", "COCKPIT_ABSENT",
		"COCKPIT_PRESENT", "COCKPIT_PRESENT",
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, hyperspaceJumpInputs(), caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"ARRIVED_IN_SUPERCRUISE"`) ||
		!contains(string(output), `"arrivalBrakeSent":true`) || caller.systemLocks != 1 || caller.hudCalls != 2 {
		t.Fatalf("output=%s locks=%d hud=%d", output, caller.systemLocks, caller.hudCalls)
	}
	if !equalInt64s(caller.throttles, []int64{100, 0}) {
		t.Fatalf("throttles=%v", caller.throttles)
	}
	joined := joinEventPhases(reporter.payloads)
	if !contains(joined, `"phase":"ARRIVAL_BRAKE"`) || !contains(joined, `"lastCommand":"SET_THROTTLE_0"`) {
		t.Fatalf("arrival brake event missing: %s", joined)
	}
}

func TestEliteHyperspaceJumpAcceptsJournalFSDJumpWhenTunnelFramesAreMissed(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{
		hyperspaceStates: []string{"COCKPIT_PRESENT", "FSD_CHARGING", "COCKPIT_PRESENT", "COCKPIT_PRESENT", "COCKPIT_PRESENT"},
		journalArrival:   true,
	}
	inputs := hyperspaceJumpInputs()
	inputs["targetLockConfirmed"] = true
	inputs["targetSystemAddress"] = int64(123)
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"arrivalEvidence":"JOURNAL_FSDJUMP"`) ||
		!contains(string(output), `"cockpitReturnConfirmations":0`) {
		t.Fatalf("output=%s", output)
	}
	if !equalInt64s(caller.throttles, []int64{100, 0}) || caller.hudCalls != 2 {
		t.Fatalf("throttles=%v hudCalls=%d", caller.throttles, caller.hudCalls)
	}
}

func TestEliteHyperspaceJumpSupercruiseStartUsesSupercruiseProfile(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{hyperspaceStates: []string{
		"COCKPIT_PRESENT", "FSD_CHARGING", "COCKPIT_ABSENT", "COCKPIT_ABSENT",
		"COCKPIT_PRESENT", "COCKPIT_PRESENT",
	}}
	inputs := hyperspaceJumpInputs()
	inputs["startMode"] = "SUPERCRUISE"
	inputs["normalSpaceConfirmed"] = false
	inputs["supercruiseConfirmed"] = true
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, inputs, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.alignProfiles) == 0 || caller.alignProfiles[0] != "SUPERCRUISE_ASSIST" {
		t.Fatalf("alignProfiles=%v", caller.alignProfiles)
	}
}

func TestEliteHyperspaceJumpEscapesStellarObstructionBeforeFSD(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &interSystemTransitCaller{
		hyperspaceStates: []string{"COCKPIT_PRESENT", "FSD_CHARGING", "COCKPIT_ABSENT", "COCKPIT_ABSENT", "COCKPIT_PRESENT", "COCKPIT_PRESENT"},
		occlusionStates:  []string{"BLOCKING", "CLEAR", "CLEAR"},
	}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, hyperspaceJumpInputs(), caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.occlusionEscapes != 1 || len(caller.alignProfiles) != 2 || caller.alignProfiles[1] != "SUPERCRUISE_ASSIST" {
		t.Fatalf("escapes=%d alignProfiles=%v", caller.occlusionEscapes, caller.alignProfiles)
	}
	if caller.hyperspaceControls != 1 {
		t.Fatalf("hyperspaceControls=%d", caller.hyperspaceControls)
	}
}

func TestEliteHyperspaceJumpFailsClosedWithoutCharging(t *testing.T) {
	pkg, err := Load(hyperspaceJumpPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	states := make([]string, 161)
	for index := range states {
		states[index] = "COCKPIT_PRESENT"
	}
	caller := &interSystemTransitCaller{hyperspaceStates: states}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, hyperspaceJumpInputs(), caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "FSD charging followed by stable hyperspace cockpit absence was not confirmed") {
		t.Fatalf("error=%v", err)
	}
	if !equalInt64s(caller.throttles, []int64{0}) {
		t.Fatalf("failure compensation throttles=%v", caller.throttles)
	}
	if caller.hyperspaceControls != 2 {
		t.Fatalf("hyperspaceControls=%d, want initial plus one bounded retry", caller.hyperspaceControls)
	}
}
