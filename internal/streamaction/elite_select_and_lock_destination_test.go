package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type selectAndLockDestinationCaller struct {
	contacts []string
	regions  []json.RawMessage
	buttons  []json.RawMessage
	details  []json.RawMessage
	controls []string
}

func (c *selectAndLockDestinationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/left-panel-tab-state":
		if len(c.contacts) == 0 {
			return nil, errors.New("unexpected Contacts observation")
		}
		state := c.contacts[0]
		c.contacts = c.contacts[1:]
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/navigation-list-text-regions":
		if len(c.regions) == 0 {
			return nil, errors.New("unexpected Navigation OCR observation")
		}
		value := c.regions[0]
		c.regions = c.regions[1:]
		return value, nil
	case "elite-dangerous/lock-destination-button-state":
		if len(c.buttons) == 0 {
			return nil, errors.New("unexpected lock button observation")
		}
		value := c.buttons[0]
		c.buttons = c.buttons[1:]
		return value, nil
	case "elite-dangerous/lock-destination-text-regions":
		if len(c.details) == 0 {
			return nil, errors.New("unexpected lock detail observation")
		}
		value := c.details[0]
		c.details = c.details[1:]
		return value, nil
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected select-and-lock-destination child Action: " + id)
	}
}

func navigationTargetRow(text string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"regions": []any{map[string]any{
			"detectionConfidence":   0.92,
			"recognitionConfidence": 0.98,
			"text":                  text,
			"referencePoints": []any{
				map[string]any{"x": 520, "y": 480}, map[string]any{"x": 700, "y": 480},
				map[string]any{"x": 700, "y": 510}, map[string]any{"x": 520, "y": 510},
			},
			"leftContext": map[string]any{"w": 1, "h": 1, "pixels": []any{0}},
		}},
	})
	return value
}

func navigationRows(targetText string, targetY int, focusedText string, focusedY int) json.RawMessage {
	row := func(text string, y int, focused bool) map[string]any {
		pixel := 0
		if focused {
			pixel = 0xff9000
		}
		return map[string]any{
			"detectionConfidence": 0.92, "recognitionConfidence": 0.98, "text": text,
			"referencePoints": []any{
				map[string]any{"x": 520, "y": y}, map[string]any{"x": 700, "y": y},
				map[string]any{"x": 700, "y": y + 30}, map[string]any{"x": 520, "y": y + 30},
			},
			"leftContext": map[string]any{"w": 1, "h": 1, "pixels": []any{pixel}},
		}
	}
	regions := []any{row(targetText, targetY, targetText == focusedText)}
	if targetText != focusedText {
		regions = append(regions, row(focusedText, focusedY, true))
	}
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "regions": regions})
	return value
}

func navigationRowsWithFocusRatios(targetText string, targetY int, targetSamples int, focusedText string, focusedY int, focusedSamples int) json.RawMessage {
	row := func(text string, y int, orangeSamples int) map[string]any {
		pixels := make([]any, 80)
		for index := range pixels {
			pixels[index] = 0
		}
		for index := 0; index < orangeSamples && index < 10; index++ {
			pixels[index*8] = 0xff9000
		}
		return map[string]any{
			"detectionConfidence": 0.92, "recognitionConfidence": 0.98, "text": text,
			"referencePoints": []any{
				map[string]any{"x": 520, "y": y}, map[string]any{"x": 700, "y": y},
				map[string]any{"x": 700, "y": y + 30}, map[string]any{"x": 520, "y": y + 30},
			},
			"leftContext": map[string]any{"w": 80, "h": 1, "pixels": pixels},
		}
	}
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "regions": []any{
		row(targetText, targetY, targetSamples), row(focusedText, focusedY, focusedSamples),
	}})
	return value
}

func loadEliteSelectAndLockDestinationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "select-and-lock-destination"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteSelectAndLockDestinationMovesHighlightBeforeSelectingTarget(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"ABSENT", "ABSENT", "NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		regions: []json.RawMessage{
			navigationRows("MOONGLOW CITY", 460, "< NAV BEACON >", 520), navigationRows("MOONGLOW CITY", 460, "< NAV BEACON >", 520),
			navigationRows("MOONGLOW CITY", 460, "MOONGLOW CITY", 460), navigationRows("MOONGLOW CITY", 460, "MOONGLOW CITY", 460),
			navigationRows("< MOONGLOW CITY >", 460, "< MOONGLOW CITY >", 460), navigationRows("< MOONGLOW CITY >", 460, "< MOONGLOW CITY >", 460),
		},
		buttons: []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details: []json.RawMessage{lockDestinationOCR("LOCK DESTINATION"), lockDestinationOCR("LOCK DESTINATION")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "MOONGLOW CITY"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "UP", "SELECT", "SELECT", "FOCUS_LEFT_PANEL"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("controls=%v want=%v", caller.controls, wantControls)
	}
	if !contains(string(output), `"result":"ACQUIRED"`) || !contains(string(output), `"navigationCount":1`) || !contains(string(output), `"rowSelectSent":true`) || !contains(string(output), `"lockSelectSent":true`) || !contains(string(output), `"restoredView":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectAndLockDestinationAcceptsStableSingleBracketLockEvidence(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		regions: []json.RawMessage{
			navigationRows("LHS 163", 460, "LHS 163", 460), navigationRows("LHS 163", 460, "LHS 163", 460),
			navigationRows("< LHS 163", 460, "< LHS 163", 460), navigationRows("< LHS 163", 460, "< LHS 163", 460),
		},
		buttons: []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details: []json.RawMessage{lockDestinationOCR("LOCK DESTINATION"), lockDestinationOCR("LOCK DESTINATION")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "LHS 163"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"ACQUIRED"`) || !contains(string(output), `"bracketEvidence":"LEADING_ONLY"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectAndLockDestinationAcceptsUniqueExactNameBesideSimilarSystem(t *testing.T) {
	rows := func(targetText string, focusedText string) json.RawMessage {
		value := navigationRows(targetText, 460, focusedText, 460)
		var decoded map[string]any
		if err := json.Unmarshal(value, &decoded); err != nil {
			t.Fatal(err)
		}
		regions := decoded["regions"].([]any)
		regions = append(regions, map[string]any{
			"detectionConfidence": 0.92, "recognitionConfidence": 0.98, "text": "TASCHETER SECTOR AG-O A6-1",
			"referencePoints": []any{
				map[string]any{"x": 520, "y": 520}, map[string]any{"x": 800, "y": 520},
				map[string]any{"x": 800, "y": 550}, map[string]any{"x": 520, "y": 550},
			},
			"leftContext": map[string]any{"w": 1, "h": 1, "pixels": []any{0}},
		})
		decoded["regions"] = regions
		result, _ := json.Marshal(decoded)
		return result
	}
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		regions: []json.RawMessage{
			rows("TASCHETER SECTOR BG-O A6-1", "TASCHETER SECTOR BG-O A6-1"), rows("TASCHETER SECTOR BG-O A6-1", "TASCHETER SECTOR BG-O A6-1"),
			navigationRows("< TASCHETER SECTOR BG-O A6-1", 460, "< TASCHETER SECTOR BG-O A6-1", 460), navigationRows("< TASCHETER SECTOR BG-O A6-1", 460, "< TASCHETER SECTOR BG-O A6-1", 460),
		},
		buttons: []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details: []json.RawMessage{lockDestinationOCR("LOCK DESTINATION"), lockDestinationOCR("LOCK DESTINATION")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "TASCHETER SECTOR BG-O A6-1"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"ACQUIRED"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectAndLockDestinationUsesUniqueRelativeFocusBelowOldThreshold(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"NAVIGATION", "NAVIGATION", "ABSENT", "ABSENT"},
		regions: []json.RawMessage{
			navigationRowsWithFocusRatios("NLTT 8662", 520, 3, "TASCHETER SECTOR AG-O A6-1", 460, 5),
			navigationRowsWithFocusRatios("NLTT 8662", 520, 3, "TASCHETER SECTOR AG-O A6-1", 460, 5),
			navigationRowsWithFocusRatios("NLTT 8662", 520, 5, "TASCHETER SECTOR AG-O A6-1", 460, 3),
			navigationRowsWithFocusRatios("NLTT 8662", 520, 5, "TASCHETER SECTOR AG-O A6-1", 460, 3),
			navigationRows("< NLTT 8662", 520, "< NLTT 8662", 520), navigationRows("< NLTT 8662", 520, "< NLTT 8662", 520),
		},
		buttons: []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details: []json.RawMessage{lockDestinationOCR("LOCK DESTINATION"), lockDestinationOCR("LOCK DESTINATION")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "NLTT 8662"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"DOWN", "SELECT", "SELECT", "FOCUS_LEFT_PANEL"}
	if !equalStrings(caller.controls, wantControls) || !contains(string(output), `"result":"ACQUIRED"`) {
		t.Fatalf("controls=%v output=%s", caller.controls, output)
	}
}

func TestEliteSelectAndLockDestinationFailsWithoutNamedVisibleTarget(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{"NAVIGATION", "NAVIGATION"},
		regions: []json.RawMessage{
			navigationTargetRow("LHS 6050"), navigationTargetRow("LHS 6050"),
			navigationTargetRow("LHS 6050"), navigationTargetRow("LHS 6050"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "named Navigation target did not produce two consecutive known observations") {
		t.Fatalf("error=%v", err)
	}
	for _, control := range caller.controls {
		if control == "SELECT" {
			t.Fatalf("missing target triggered SELECT: controls=%v", caller.controls)
		}
	}
}

func TestEliteSelectAndLockDestinationUsesObservedTabStatesToReachNavigation(t *testing.T) {
	caller := &selectAndLockDestinationCaller{
		contacts: []string{
			"CONTACTS", "CONTACTS",
			"SYSTEM", "SYSTEM",
			"NAVIGATION", "NAVIGATION",
			"ABSENT", "ABSENT",
		},
		regions: []json.RawMessage{navigationRows("< NAV BEACON >", 480, "< NAV BEACON >", 480), navigationRows("< NAV BEACON >", 480, "< NAV BEACON >", 480)},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSelectAndLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"NEXT_PANEL", "NEXT_PANEL", "FOCUS_LEFT_PANEL"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("controls=%v want=%v", caller.controls, wantControls)
	}
	if !contains(string(output), `"result":"EXISTING"`) || !contains(string(output), `"openedPanel":false`) || !contains(string(output), `"restoredView":true`) {
		t.Fatalf("output=%s", output)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
