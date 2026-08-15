package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type pauseAtExitCaller struct {
	controls []string
	failAt   int
}

func (c *pauseAtExitCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	if id != "elite-dangerous/ui-control" {
		return nil, errors.New("unexpected child: " + id)
	}
	control, _ := inputs["control"].(string)
	c.controls = append(c.controls, control)
	if c.failAt > 0 && len(c.controls) == c.failAt {
		return nil, errors.New("injected input failure")
	}
	return json.RawMessage(`{"schemaVersion":1}`), nil
}

func loadElitePauseAtExitPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "pause-at-exit-for-human-takeover"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestElitePauseAtExitReplaysExactReviewedKeyStructureWithoutObservation(t *testing.T) {
	caller := &pauseAtExitCaller{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PAUSE", "DOWN", "DOWN", "DOWN", "DOWN", "DOWN", "SELECT", "SELECT"}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls=%v want=%v", caller.controls, want)
	}
	for _, forbidden := range []string{"pause-menu-text-regions", "exit-destination-menu-text-regions", "mainMenuConfirmed"} {
		if contains(string(output), forbidden) {
			t.Fatalf("output contains observation claim %q: %s", forbidden, output)
		}
	}
	if !contains(string(output), `"keyReplayCompleted":true`) || !contains(string(output), `"visualPostconditionClaimed":false`) || !contains(string(output), `"sequenceLength":8`) {
		t.Fatalf("output=%s", output)
	}
}

func TestElitePauseAtExitStopsAtFirstInputFailure(t *testing.T) {
	caller := &pauseAtExitCaller{failAt: 4}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "injected input failure") {
		t.Fatalf("err=%v", err)
	}
	want := []string{"PAUSE", "DOWN", "DOWN", "DOWN"}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls after failure=%v want=%v", caller.controls, want)
	}
}
