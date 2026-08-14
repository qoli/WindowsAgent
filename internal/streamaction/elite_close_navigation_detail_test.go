package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type closeNavigationDetailCaller struct {
	states          []string
	panelStates     []string
	detailRegions   []json.RawMessage
	controls        []string
	closePanelCalls int
	closePanelOut   json.RawMessage
}

func (c *closeNavigationDetailCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/navigation-tab-transition-state":
		if len(c.states) == 0 {
			return nil, errors.New("unexpected left-panel observation")
		}
		state := c.states[0]
		c.states = c.states[1:]
		return json.Marshal(map[string]any{"state": state})
	case "elite-dangerous/left-panel-tab-state":
		if len(c.panelStates) == 0 {
			return nil, errors.New("unexpected full panel observation")
		}
		state := c.panelStates[0]
		c.panelStates = c.panelStates[1:]
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/lock-destination-text-regions":
		if len(c.detailRegions) == 0 {
			return nil, errors.New("unexpected detail OCR observation")
		}
		value := c.detailRegions[0]
		c.detailRegions = c.detailRegions[1:]
		return value, nil
	case "elite-dangerous/close-left-panel":
		c.closePanelCalls++
		if c.closePanelOut != nil {
			return c.closePanelOut, nil
		}
		return json.RawMessage(`{"schemaVersion":1,"closed":true,"commandSent":true,"initialState":"NAVIGATION","finalState":"ABSENT"}`), nil
	default:
		return nil, errors.New("unexpected close-navigation-detail child Action: " + id)
	}
}

func loadEliteCloseNavigationDetailPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "close-navigation-detail"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEliteCloseNavigationDetailRequiresListBeforeClosingPanel(t *testing.T) {
	caller := &closeNavigationDetailCaller{states: []string{
		"ABSENT", "UNKNOWN", "ABSENT", "ABSENT", "ABSENT", "ABSENT",
		"UNKNOWN", "ABSENT", "ABSENT", "UNKNOWN", "ABSENT", "ABSENT",
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "BACK did not produce a confirmed NAVIGATION list transition") {
		t.Fatalf("error=%v", err)
	}
	if !equalStrings(caller.controls, []string{"BACK"}) || caller.closePanelCalls != 0 {
		t.Fatalf("controls=%v closePanelCalls=%d", caller.controls, caller.closePanelCalls)
	}
}

func TestEliteCloseNavigationDetailAcceptsObservedListThenAutomaticClose(t *testing.T) {
	caller := &closeNavigationDetailCaller{
		states:        []string{"UNKNOWN", "NAVIGATION"},
		closePanelOut: json.RawMessage(`{"schemaVersion":1,"closed":true,"commandSent":false,"initialState":"ABSENT","finalState":"ABSENT"}`),
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.controls, []string{"BACK"}) || caller.closePanelCalls != 1 {
		t.Fatalf("controls=%v closePanelCalls=%d", caller.controls, caller.closePanelCalls)
	}
	if !contains(string(output), `"listConfirmed":true`) ||
		!contains(string(output), `"panelClosed":true`) ||
		!contains(string(output), `"finalState":"ABSENT"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteCloseNavigationDetailConfirmsListThenForwardView(t *testing.T) {
	caller := &closeNavigationDetailCaller{states: []string{"NAVIGATION"}}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.controls, []string{"BACK"}) || caller.closePanelCalls != 1 {
		t.Fatalf("controls=%v closePanelCalls=%d", caller.controls, caller.closePanelCalls)
	}
	if !contains(string(output), `"listConfirmed":true`) ||
		!contains(string(output), `"panelClosed":true`) ||
		!contains(string(output), `"finalState":"ABSENT"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteCloseNavigationDetailAcceptsConfirmedAutomaticCloseWhenTransientListWasMissed(t *testing.T) {
	empty := json.RawMessage(`{"regions":[]}`)
	caller := &closeNavigationDetailCaller{
		states: []string{
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
		},
		panelStates:   []string{"ABSENT", "ABSENT"},
		detailRegions: []json.RawMessage{empty, empty},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{"detailLabelConfirmed": true}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.controls, []string{"BACK"}) || caller.closePanelCalls != 0 {
		t.Fatalf("controls=%v closePanelCalls=%d", caller.controls, caller.closePanelCalls)
	}
	if !contains(string(output), `"listConfirmed":false`) ||
		!contains(string(output), `"autoClosedAfterBack":true`) ||
		!contains(string(output), `"panelClosed":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteCloseNavigationDetailRejectsAbsentTabsWhileDetailLabelRemains(t *testing.T) {
	present := json.RawMessage(`{"regions":[{"detectionConfidence":0.9,"recognitionConfidence":0.9,"text":"SUPERCRUISE ASSIST"}]}`)
	caller := &closeNavigationDetailCaller{
		states: []string{
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
			"NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED", "NOT_CONFIRMED",
		},
		panelStates:   []string{"ABSENT", "ABSENT", "ABSENT", "ABSENT"},
		detailRegions: []json.RawMessage{present, present, present, present},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{"detailLabelConfirmed": true}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "did not produce a confirmed list or automatic forward-view transition") {
		t.Fatalf("error=%v", err)
	}
}

func TestEliteCloseNavigationDetailRejectsInconsistentPanelCloseResult(t *testing.T) {
	caller := &closeNavigationDetailCaller{
		states:        []string{"NAVIGATION", "NAVIGATION"},
		closePanelOut: json.RawMessage(`{"schemaVersion":1,"closed":false,"commandSent":true,"initialState":"NAVIGATION","finalState":"NAVIGATION"}`),
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteCloseNavigationDetailPackage(t), map[string]any{}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "Navigation list did not close to the forward view") {
		t.Fatalf("error=%v", err)
	}
}
