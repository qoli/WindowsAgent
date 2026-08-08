package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type lockDestinationCaller struct {
	buttons  []json.RawMessage
	details  []json.RawMessage
	navigation []json.RawMessage
	controls []string
}

func (c *lockDestinationCaller) next(values *[]json.RawMessage, name string) (json.RawMessage, error) {
	if len(*values) == 0 {
		return nil, errors.New("unexpected " + name + " observation")
	}
	value := (*values)[0]
	*values = (*values)[1:]
	return value, nil
}

func (c *lockDestinationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/lock-destination-button-state":
		return c.next(&c.buttons, "button")
	case "elite-dangerous/lock-destination-text-regions":
		return c.next(&c.details, "detail OCR")
	case "elite-dangerous/navigation-list-text-regions":
		return c.next(&c.navigation, "Navigation list OCR")
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1,"selection":"SELECT"}`), nil
	default:
		return nil, errors.New("unexpected lock-destination child Action: " + id)
	}
}

func lockDestinationButton(state string) json.RawMessage {
	focused := state == "FOCUSED"
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"button":        map[string]any{"state": state, "focused": focused, "highlightRatio": 0.84},
	})
	return value
}

func lockDestinationOCR(text string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"regions": []any{map[string]any{
			"detectionConfidence": 0.92, "recognitionConfidence": 0.98, "text": text,
			"referencePoints": []any{
				map[string]any{"x": 480, "y": 700}, map[string]any{"x": 640, "y": 700},
				map[string]any{"x": 640, "y": 730}, map[string]any{"x": 480, "y": 730},
			},
		}},
		"timing": map[string]any{"ocrTotalMs": 50},
	})
	return value
}

func lockedNavigationRow(text string) json.RawMessage {
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"regions": []any{map[string]any{
			"detectionConfidence": 0.92, "recognitionConfidence": 0.98, "text": text,
			"referencePoints": []any{
				map[string]any{"x": 520, "y": 480}, map[string]any{"x": 650, "y": 480},
				map[string]any{"x": 650, "y": 510}, map[string]any{"x": 520, "y": 510},
			},
		}},
	})
	return value
}

func loadEliteLockDestinationPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "lock-destination"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteLockDestinationSelectsOnceAndVerifiesNamedBracketedRow(t *testing.T) {
	caller := &lockDestinationCaller{
		buttons:  []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details:  []json.RawMessage{lockDestinationOCR("LOCK DESTINATION"), lockDestinationOCR("LOCK DESTINATION")},
		navigation: []json.RawMessage{lockedNavigationRow("< NAV BEACON >"), lockedNavigationRow("< NAV BEACON >")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 1 || caller.controls[0] != "SELECT" {
		t.Fatalf("controls=%v", caller.controls)
	}
	if !contains(string(output), `"result":"ACQUIRED"`) || !contains(string(output), `"targetLocked":true`) || !contains(string(output), `"selectSent":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteLockDestinationDoesNotToggleExistingLock(t *testing.T) {
	caller := &lockDestinationCaller{
		buttons: []json.RawMessage{lockDestinationButton("FOCUSED"), lockDestinationButton("FOCUSED")},
		details: []json.RawMessage{lockDestinationOCR("UNLOCK DESTINATION"), lockDestinationOCR("UNLOCK DESTINATION")},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteLockDestinationPackage(t), map[string]any{"targetName": "NAV BEACON"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 || !contains(string(output), `"result":"EXISTING"`) || !contains(string(output), `"selectSent":false`) {
		t.Fatalf("controls=%v output=%s", caller.controls, output)
	}
}
