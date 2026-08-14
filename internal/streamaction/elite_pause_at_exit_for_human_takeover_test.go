package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type pauseAtExitCaller struct {
	observations []json.RawMessage
	index        int
	controls     []string
}

func pauseMenuRaw(state string) json.RawMessage {
	regions := []any{}
	if state != "ABSENT" {
		regions = append(regions,
			map[string]any{"text": "RESUME", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": []any{0}}},
			map[string]any{"text": "EXIT", "detectionConfidence": 0.98, "recognitionConfidence": 0.99, "leftContext": map[string]any{"pixels": focusedPixels(state == "EXIT_FOCUSED")}},
		)
	}
	value, _ := json.Marshal(map[string]any{"regions": regions})
	return value
}

func (c *pauseAtExitCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/pause-menu-text-regions":
		if c.index >= len(c.observations) {
			return nil, errors.New("unexpected pause observation")
		}
		value := c.observations[c.index]
		c.index++
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

func TestElitePauseAtExitOpensMenuAndStopsWithoutSelecting(t *testing.T) {
	caller := &pauseAtExitCaller{observations: []json.RawMessage{
		pauseMenuRaw("ABSENT"), pauseMenuRaw("PAUSE_MENU"), pauseMenuRaw("EXIT_FOCUSED"), pauseMenuRaw("EXIT_FOCUSED"),
	}}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), loadElitePauseAtExitPackage(t), map[string]any{}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"exitFocused":true`) || !contains(string(output), `"selectSent":false`) {
		t.Fatalf("output=%s", output)
	}
	if len(caller.controls) != 9 || caller.controls[0] != "BACK" {
		t.Fatalf("controls=%v", caller.controls)
	}
	for _, control := range caller.controls {
		if control == "SELECT" {
			t.Fatalf("handoff selected EXIT: controls=%v", caller.controls)
		}
	}
}
