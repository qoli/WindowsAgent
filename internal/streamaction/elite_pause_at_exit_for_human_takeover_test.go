package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type pauseAtExitCaller struct {
	primary      []json.RawMessage
	destinations []json.RawMessage
	primaryIndex int
	destIndex    int
	controls     []string
}

func rawTextRegions(regions []any) json.RawMessage {
	value, _ := json.Marshal(map[string]any{"regions": regions})
	return value
}

func pauseMenuRaw(state string) json.RawMessage {
	regions := []any{}
	if state == "UNKNOWN" {
		regions = append(regions, map[string]any{"text": "RESUME", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": []any{0}}})
	} else if state == "MAIN_MENU" {
		for _, text := range []string{"CONTINUE", "SOCIAL", "GAME EXTRAS", "OPTIONS"} {
			regions = append(regions, map[string]any{"text": text, "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": []any{0}}})
		}
	} else if state != "ABSENT" {
		exitPixels := focusedPixels(state == "EXIT_FOCUSED")
		if state == "EXIT_FOCUSED_LOW_FILL" {
			exitPixels = make([]any, 100)
			for index := range exitPixels {
				exitPixels[index] = uint32(0)
			}
			exitPixels[0] = uint32(0xFF7700)
		}
		regions = append(regions,
			map[string]any{"text": "RESUME", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": []any{0}}},
			map[string]any{"text": "EXIT", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": exitPixels}},
		)
	}
	return rawTextRegions(regions)
}

func exitDestinationRaw(state string) json.RawMessage {
	if state == "ABSENT" {
		return rawTextRegions([]any{})
	}
	regions := []any{
		map[string]any{"text": "EXIT TO MAIN MENU", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": focusedPixels(state == "MAIN_FOCUSED" || state == "BOTH_FOCUSED")}},
		map[string]any{"text": "QUIT TO DESKTOP", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": focusedPixels(state == "DESKTOP_FOCUSED" || state == "BOTH_FOCUSED")}},
	}
	return rawTextRegions(regions)
}

func (c *pauseAtExitCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/pause-menu-text-regions":
		if c.primaryIndex >= len(c.primary) {
			return nil, errors.New("unexpected primary menu observation")
		}
		value := c.primary[c.primaryIndex]
		c.primaryIndex++
		return value, nil
	case "elite-dangerous/exit-destination-menu-text-regions":
		if c.destIndex >= len(c.destinations) {
			return nil, errors.New("unexpected exit destination observation")
		}
		value := c.destinations[c.destIndex]
		c.destIndex++
		return value, nil
	case "elite-dangerous/ui-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected child: " + id)
	}
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

func TestElitePauseAtExitSelectsReviewedMainMenuAndConfirmsNonFlightMenu(t *testing.T) {
	caller := &pauseAtExitCaller{
		primary: []json.RawMessage{
			pauseMenuRaw("ABSENT"), pauseMenuRaw("PAUSE_MENU"),
			pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("EXIT_FOCUSED_LOW_FILL"),
			pauseMenuRaw("EXIT_FOCUSED_LOW_FILL"), pauseMenuRaw("EXIT_FOCUSED_LOW_FILL"),
			pauseMenuRaw("ABSENT"), pauseMenuRaw("MAIN_MENU"), pauseMenuRaw("MAIN_MENU"),
		},
		destinations: []json.RawMessage{exitDestinationRaw("MAIN_FOCUSED"), exitDestinationRaw("MAIN_FOCUSED")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"mainMenuConfirmed":true`) || !contains(string(output), `"exitToMainMenuSelectSent":true`) {
		t.Fatalf("output=%s", output)
	}
	want := []string{"PAUSE", "DOWN", "DOWN", "DOWN", "DOWN", "DOWN", "SELECT", "SELECT"}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls=%v want=%v", caller.controls, want)
	}
}

func TestElitePauseAtExitRejectsUnknownWithoutInput(t *testing.T) {
	caller := &pauseAtExitCaller{
		primary: []json.RawMessage{pauseMenuRaw("UNKNOWN")},
		destinations: []json.RawMessage{
			exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"),
			exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"), exitDestinationRaw("ABSENT"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "PAUSE_MENU_STATE_UNKNOWN") {
		t.Fatalf("err=%v", err)
	}
	if len(caller.controls) != 0 {
		t.Fatalf("controls=%v", caller.controls)
	}
}

func TestElitePauseAtExitResumesFromReviewedSecondLevelMenu(t *testing.T) {
	caller := &pauseAtExitCaller{
		primary:      []json.RawMessage{pauseMenuRaw("UNKNOWN"), pauseMenuRaw("ABSENT"), pauseMenuRaw("MAIN_MENU"), pauseMenuRaw("MAIN_MENU")},
		destinations: []json.RawMessage{exitDestinationRaw("MAIN_FOCUSED"), exitDestinationRaw("MAIN_FOCUSED")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"mainMenuConfirmed":true`) || !equalStrings(caller.controls, []string{"SELECT"}) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
}

func TestElitePauseAtExitRejectsAmbiguousSecondLevelFocus(t *testing.T) {
	caller := &pauseAtExitCaller{
		primary: []json.RawMessage{pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED")},
		destinations: []json.RawMessage{
			exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"),
			exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"), exitDestinationRaw("BOTH_FOCUSED"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "EXIT_DESTINATION_MAIN_MENU_NOT_CONFIRMED") {
		t.Fatalf("err=%v", err)
	}
	if !equalStrings(caller.controls, []string{"SELECT"}) {
		t.Fatalf("ambiguous second-level focus received another input: %v", caller.controls)
	}
}

func TestElitePauseAtExitDoesNotRetoggleWhenMenuIsNotConfirmed(t *testing.T) {
	caller := &pauseAtExitCaller{primary: []json.RawMessage{pauseMenuRaw("ABSENT"), pauseMenuRaw("ABSENT")}}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "PAUSE_MENU_NOT_CONFIRMED") {
		t.Fatalf("err=%v", err)
	}
	if !equalStrings(caller.controls, []string{"PAUSE"}) {
		t.Fatalf("controls=%v", caller.controls)
	}
}

func TestElitePauseAtExitRequiresStableFirstLevelFocusBeforeAnySelect(t *testing.T) {
	caller := &pauseAtExitCaller{primary: []json.RawMessage{
		pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("EXIT_FOCUSED"),
		pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("EXIT_FOCUSED"),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "PAUSE_MENU_EXIT_NOT_FOCUSED") {
		t.Fatalf("err=%v", err)
	}
	for _, control := range caller.controls {
		if control == "SELECT" {
			t.Fatalf("unstable EXIT focus authorized SELECT: %v", caller.controls)
		}
	}
}

func TestElitePauseAtExitNeverBlindSelectsUnconfirmedSecondLevel(t *testing.T) {
	caller := &pauseAtExitCaller{
		primary: []json.RawMessage{pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED")},
		destinations: []json.RawMessage{
			exitDestinationRaw("ABSENT"), exitDestinationRaw("DESKTOP_FOCUSED"), exitDestinationRaw("ABSENT"), exitDestinationRaw("DESKTOP_FOCUSED"),
			exitDestinationRaw("ABSENT"), exitDestinationRaw("DESKTOP_FOCUSED"), exitDestinationRaw("ABSENT"), exitDestinationRaw("DESKTOP_FOCUSED"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "EXIT_DESTINATION_MAIN_MENU_NOT_CONFIRMED") {
		t.Fatalf("err=%v", err)
	}
	if !equalStrings(caller.controls, []string{"SELECT"}) {
		t.Fatalf("second-level menu received blind input: %v", caller.controls)
	}
}

func TestElitePauseAtExitRequiresMainMenuAfterSecondSelect(t *testing.T) {
	primary := []json.RawMessage{pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED")}
	for index := 0; index < 120; index++ {
		primary = append(primary, pauseMenuRaw("ABSENT"))
	}
	caller := &pauseAtExitCaller{
		primary:      primary,
		destinations: []json.RawMessage{exitDestinationRaw("MAIN_FOCUSED"), exitDestinationRaw("MAIN_FOCUSED")},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "MAIN_MENU_NOT_CONFIRMED_AFTER_EXIT") {
		t.Fatalf("err=%v", err)
	}
	if !equalStrings(caller.controls, []string{"SELECT", "SELECT"}) {
		t.Fatalf("post-exit verification repeated input: %v", caller.controls)
	}
}
