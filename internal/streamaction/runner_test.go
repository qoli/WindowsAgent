package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

type fixtureCaller struct{ calls int }

type failureActionCaller struct {
	calls       []string
	failCleanup bool
}

func (f *failureActionCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, id)
	if id == "game/fail" {
		return nil, errors.New("primary failure")
	}
	if id == "game/stop" {
		if inputs["percent"] != int64(0) {
			return nil, errors.New("unexpected cleanup input")
		}
		if f.failCleanup {
			return nil, errors.New("cleanup failure")
		}
		return json.RawMessage(`{"stopped":true}`), nil
	}
	return nil, errors.New("unexpected child Action")
}

func (f *fixtureCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	f.calls++
	if id != "game/status" || inputs["detail"] != true {
		return nil, errors.New("unexpected child Action call")
	}
	return json.RawMessage(`{"massLock":"OFF"}`), nil
}

type fixtureReporter struct {
	types    []string
	payloads []json.RawMessage
}

type contactsPanelCaller struct {
	states   []string
	index    int
	controls []string
}

type stationTargetCaller struct {
	contactsStates []string
	contactsIndex  int
	regions        []json.RawMessage
	regionIndex    int
	controls       []string
}

type dockAtStationCaller struct {
	contactsStates       []string
	requestStates        []string
	flightStates         []string
	gearStates           []string
	rangeState           string
	rangeStates          []string
	rangeDistances       []float64
	rangeIndex           int
	contactsIndex        int
	requestIndex         int
	flightIndex          int
	gearIndex            int
	flightPromptFailures int
	rangeCalls           int
	controls             []string
}

func (c *dockAtStationCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/left-panel-tab-state":
		if c.contactsIndex >= len(c.contactsStates) {
			return nil, errors.New("unexpected Contacts observation")
		}
		state := c.contactsStates[c.contactsIndex]
		c.contactsIndex++
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/request-docking-range":
		c.rangeCalls++
		state := c.rangeState
		rangeIndex := c.rangeIndex
		if c.rangeStates != nil {
			if c.rangeIndex >= len(c.rangeStates) {
				return nil, errors.New("unexpected range observation")
			}
			state = c.rangeStates[c.rangeIndex]
		}
		distance := any(nil)
		reason := "OCR_CONFIDENCE_LOW"
		if state == "ALLOWED" {
			distance = 5390.0
			reason = "DISPLAY_DISTANCE_BELOW_THRESHOLD"
		} else if state == "DENIED" {
			distance = 9000.0
			reason = "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"
		}
		if c.rangeDistances != nil && state != "UNKNOWN" {
			if rangeIndex >= len(c.rangeDistances) {
				return nil, errors.New("unexpected range distance observation")
			}
			distance = c.rangeDistances[rangeIndex]
		}
		c.rangeIndex++
		return json.Marshal(map[string]any{"requestDockingRange": map[string]any{
			"state": state, "distanceMeters": distance, "evidence": map[string]any{"reason": reason},
		}})
	case "elite-dangerous/request-docking-availability":
		if c.requestIndex >= len(c.requestStates) {
			return nil, errors.New("unexpected Request Docking observation")
		}
		state := c.requestStates[c.requestIndex]
		c.requestIndex++
		return json.Marshal(map[string]any{
			"requestDocking": map[string]any{"state": state},
			"decision":       map[string]any{"reason": "FIXTURE_" + state},
		})
	case "elite-dangerous/ui-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/set-throttle":
		if inputs["percent"] != int64(0) && inputs["percent"] != 0 {
			return nil, errors.New("unexpected throttle input")
		}
		return json.RawMessage(`{"control":"SetSpeedZero"}`), nil
	case "elite-dangerous/flight-prompt-text":
		if c.flightPromptFailures > 0 {
			c.flightPromptFailures--
			return nil, errors.New("capture OCR Action region: primary monitor capture size is invalid")
		}
		return json.RawMessage(`{"text":""}`), nil
	case "elite-dangerous/flight-status":
		if c.flightIndex >= len(c.flightStates) {
			return nil, errors.New("unexpected flight-status observation")
		}
		state := c.flightStates[c.flightIndex]
		c.flightIndex++
		return json.Marshal(map[string]any{"flightStatus": map[string]any{"state": state}})
	case "elite-dangerous/ship-status":
		if c.gearIndex >= len(c.gearStates) {
			return nil, errors.New("unexpected ship-status observation")
		}
		state := c.gearStates[c.gearIndex]
		c.gearIndex++
		return json.Marshal(map[string]any{"shipStatus": map[string]any{"landingGear": map[string]any{"state": state}}})
	default:
		return nil, errors.New("unexpected dock-at-station child Action: " + id)
	}
}

func (c *contactsPanelCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	if id == "elite-dangerous/ui-control" {
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	}
	if id != "elite-dangerous/left-panel-tab-state" || len(inputs) != 0 || c.index >= len(c.states) {
		return nil, errors.New("unexpected select-Contacts child Action call")
	}
	state := c.states[c.index]
	c.index++
	return json.Marshal(map[string]any{
		"schemaVersion": 2,
		"activeTab":     map[string]any{"state": state},
	})
}

func (c *stationTargetCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/left-panel-tab-state":
		if len(inputs) != 0 || c.contactsIndex >= len(c.contactsStates) {
			return nil, errors.New("unexpected Station target Contacts observation")
		}
		state := c.contactsStates[c.contactsIndex]
		c.contactsIndex++
		return json.Marshal(map[string]any{"activeTab": map[string]any{"state": state}})
	case "elite-dangerous/station-contact-text-regions":
		if len(inputs) != 0 || c.regionIndex >= len(c.regions) {
			return nil, errors.New("unexpected Station target OCR observation")
		}
		result := c.regions[c.regionIndex]
		c.regionIndex++
		return result, nil
	case "elite-dangerous/ui-control":
		control, _ := inputs["control"].(string)
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected select-station-target child Action: " + id)
	}
}

func selectContactsPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "select-contacts-panel"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func selectStationTargetPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "select-station-target"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func dockAtStationPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "dock-at-station"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func (f *fixtureReporter) Emit(_ context.Context, eventType string, payload json.RawMessage) (eventstream.Event, error) {
	f.types = append(f.types, eventType)
	f.payloads = append(f.payloads, append(json.RawMessage(nil), payload...))
	return eventstream.Event{Sequence: uint64(len(f.types))}, nil
}

func TestRunnerCallsFiniteActionEmitsValidatedEventAndReturnsOutput(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    status = action.call(id="game/status", inputs={"detail": True})
    sequence = stream.emit(type="action.phase.changed", payload={"phase": "DONE", "massLock": status["massLock"]})
    task.sleep(milliseconds=1)
    return {"done": True, "sequence": sequence}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	caller := &fixtureCaller{}
	reporter := &fixtureReporter{}
	output, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, caller, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"done":true,"sequence":1}` || caller.calls != 1 || len(reporter.types) != 1 || reporter.types[0] != "action.phase.changed" {
		t.Fatalf("output=%s calls=%d events=%v", output, caller.calls, reporter.types)
	}
}

func TestRunnerEmitsHostValidatedDisplayActivity(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    sequence = stream.activity(message="Throttle set to 100%", level="info")
    return {"done": True, "sequence": sequence}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fixtureReporter{}
	if _, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, reporter); err != nil {
		t.Fatal(err)
	}
	if len(reporter.types) != 1 || reporter.types[0] != ActivityEventType || string(reporter.payloads[0]) != `{"level":"info","message":"Throttle set to 100%"}` {
		t.Fatalf("events=%v payloads=%s", reporter.types, reporter.payloads)
	}
}

func TestRunnerRejectsMalformedDisplayActivityWithoutEventFallback(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    stream.activity(message="line one\nline two", level="info")
    return {"done": True, "sequence": 1}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fixtureReporter{}
	_, err = (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, reporter)
	if err == nil || !contains(err.Error(), "one canonical non-empty line") {
		t.Fatalf("error=%v", err)
	}
	if len(reporter.types) != 0 {
		t.Fatalf("malformed activity emitted events=%v", reporter.types)
	}
}

func TestRunnerTryCallReturnsExplicitChildFailure(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    attempt = action.try_call(id="game/status", inputs={"detail": False})
    if attempt["ok"]:
        fail("failed child Action was reported as successful")
    if attempt["output"] != None:
        fail("failed child Action returned output")
    if "child Action game/status failed: unexpected child Action call" not in attempt["error"] or attempt["errorCode"] != None:
        fail("child failure was not preserved")
    return {"done": True, "sequence": 0}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"done":true,"sequence":0}` {
		t.Fatalf("output=%s", output)
	}
}

func TestRunnerTryCallReturnsSuccessfulOutput(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    attempt = action.try_call(id="game/status", inputs={"detail": True})
    if not attempt["ok"] or attempt["error"] != None or attempt["errorCode"] != None or attempt["output"]["massLock"] != "OFF":
        fail("successful child Action attempt lost its output")
    return {"done": True, "sequence": 0}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"done":true,"sequence":0}` {
		t.Fatalf("output=%s", output)
	}
}

func TestRunnerTryCallReturnsCaptureErrorCode(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    attempt = action.try_call(id="game/status", inputs={"detail": False})
    if attempt["ok"] or attempt["errorCode"] != "capture_readback_failed":
        fail("capture failure code was not preserved")
    return {"done": True, "sequence": 0}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	caller := CallerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
		return nil, capture.Failure("capture_readback_failed", "readback failed", errors.New("HRESULT"))
	})
	if _, err := (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, caller, &fixtureReporter{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerExecutesRegisteredFailureActionAndClearsItOnSuccess(t *testing.T) {
	for _, test := range []struct {
		name      string
		script    string
		wantCalls []string
		wantError string
	}{
		{
			name: "failure",
			script: `
def main(ctx):
    action.on_failure(id="game/stop", inputs={"percent": 0})
    action.call(id="game/fail", inputs={})
    return {"done": True, "sequence": 0}
`,
			wantCalls: []string{"game/fail", "game/stop"},
			wantError: "primary failure",
		},
		{
			name: "cleared",
			script: `
def main(ctx):
    action.on_failure(id="game/stop", inputs={"percent": 0})
    action.clear_on_failure()
    fail("later failure")
`,
			wantCalls: nil,
			wantError: "later failure",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixturePackage(t, test.script)
			pkg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			caller := &failureActionCaller{}
			_, err = (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, caller, &fixtureReporter{})
			if err == nil || !contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v", err)
			}
			if len(caller.calls) != len(test.wantCalls) {
				t.Fatalf("calls=%v", caller.calls)
			}
			for index := range test.wantCalls {
				if caller.calls[index] != test.wantCalls[index] {
					t.Fatalf("calls=%v", caller.calls)
				}
			}
		})
	}
}

type CallerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f CallerFunc) Call(ctx context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	return f(ctx, id, inputs)
}

func TestRunnerRejectsEventOutsidePackageSchema(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    stream.emit(type="action.invalid", payload={"phase": "DONE"})
    return {"done": True, "sequence": 1}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Runner{}).Run(context.Background(), pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
	if err == nil || !contains(err.Error(), "event schema") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerSleepIsCancelled(t *testing.T) {
	root := writeFixturePackage(t, `
def main(ctx):
    task.sleep(milliseconds=5000)
    return {"done": True, "sequence": 0}
`)
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (Runner{}).Run(ctx, pkg, map[string]any{"enabled": true}, &fixtureCaller{}, &fixtureReporter{})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled streaming Action did not stop")
	}
}

func TestEliteSelectContactsPanelIsIdempotentWhenAlreadySelected(t *testing.T) {
	pkg, err := Load(selectContactsPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &contactsPanelCaller{states: []string{"CONTACTS", "CONTACTS"}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 || !contains(string(output), `"contactsState":"SELECTED"`) ||
		!contains(string(output), `"selected":true`) ||
		!contains(string(output), `"cycleCount":0`) || !contains(string(output), `"openedPanel":false`) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
}

func TestEliteSelectContactsPanelOpensAndCyclesWithVerification(t *testing.T) {
	pkg, err := Load(selectContactsPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &contactsPanelCaller{states: []string{
		"ABSENT", "ABSENT",
		"NAVIGATION", "NAVIGATION",
		"TRANSACTIONS", "TRANSACTIONS",
		"CONTACTS", "CONTACTS",
	}}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "NEXT_PANEL", "NEXT_PANEL"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v output=%s", caller.controls, output)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
	if !contains(string(output), `"contactsState":"SELECTED"`) || !contains(string(output), `"openedPanel":true`) ||
		!contains(string(output), `"cycleCount":2`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectContactsPanelUsesThreeStepFourTabBound(t *testing.T) {
	pkg, err := Load(selectContactsPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &contactsPanelCaller{states: []string{
		"SYSTEM", "SYSTEM",
		"NAVIGATION", "NAVIGATION",
		"TRANSACTIONS", "TRANSACTIONS",
		"CONTACTS", "CONTACTS",
	}}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 3 || !contains(string(output), `"cycleCount":3`) ||
		!contains(string(output), `"contactsState":"SELECTED"`) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
}

func TestEliteSelectContactsPanelStopsOnUnknownOrExhaustedCycle(t *testing.T) {
	for _, test := range []struct {
		name   string
		states []string
		want   string
	}{
		{"unknown", []string{"UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"}, "did not produce two consecutive known observations"},
		{"exhausted", []string{
			"SYSTEM", "SYSTEM",
			"SYSTEM", "SYSTEM",
			"SYSTEM", "SYSTEM",
			"SYSTEM", "SYSTEM",
		}, "CONTACTS was not reached within three NEXT_PANEL inputs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg, err := Load(selectContactsPackageRoot(t))
			if err != nil {
				t.Fatal(err)
			}
			caller := &contactsPanelCaller{states: test.states}
			_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
				context.Background(), pkg, map[string]any{}, caller, &fixtureReporter{},
			)
			if err == nil || !contains(err.Error(), test.want) {
				t.Fatalf("error=%v controls=%v", err, caller.controls)
			}
		})
	}
}

func stationTargetRegions(t *testing.T, targetText string, targetY float64, targetFocused bool, focusedOtherY *float64) json.RawMessage {
	t.Helper()
	region := func(text string, y float64, focused bool) map[string]any {
		pixel := uint32(0x1B0C08)
		if focused {
			pixel = uint32(0x501405)
		}
		pixels := make([]any, 100*30)
		for index := range pixels {
			pixels[index] = pixel
		}
		return map[string]any{
			"points": []any{
				map[string]any{"x": 20.0, "y": 20.0}, map[string]any{"x": 180.0, "y": 20.0},
				map[string]any{"x": 180.0, "y": 40.0}, map[string]any{"x": 20.0, "y": 40.0},
			},
			"referencePoints": []any{
				map[string]any{"x": 300.0, "y": y - 10}, map[string]any{"x": 500.0, "y": y - 10},
				map[string]any{"x": 500.0, "y": y + 10}, map[string]any{"x": 300.0, "y": y + 10},
			},
			"detectionConfidence":   0.90,
			"text":                  text,
			"recognitionConfidence": 0.98,
			"leftContext": map[string]any{
				"x": int64(0), "y": int64(0), "w": int64(100), "h": int64(30), "pixels": pixels,
				"referenceRegion": map[string]any{"x": 180.0, "y": y - 15, "w": 100.0, "h": 30.0},
			},
		}
	}
	regions := []any{region(targetText, targetY, targetFocused)}
	if focusedOtherY != nil {
		regions = append(regions, region("CASPIAN EXPLORER", *focusedOtherY, true))
	}
	result, err := json.Marshal(map[string]any{
		"schemaVersion": int64(1), "regions": regions,
		"evidence": map[string]any{}, "models": map[string]any{}, "timing": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEliteSelectStationTargetReturnsExistingWithoutSelect(t *testing.T) {
	pkg, err := Load(selectStationTargetPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	locked := stationTargetRegions(t, "< MOONGLOW CITY >", 350, true, nil)
	caller := &stationTargetCaller{
		contactsStates: []string{"CONTACTS", "CONTACTS"},
		regions:        []json.RawMessage{locked, locked},
	}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{"stationName": "MoonGlow City"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.controls) != 0 || !contains(string(output), `"result":"EXISTING"`) ||
		!contains(string(output), `"selectSent":false`) || !contains(string(output), `"targetLocked":true`) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
}

func TestEliteSelectStationTargetMovesByObservedGeometryThenSelectsOnce(t *testing.T) {
	pkg, err := Load(selectStationTargetPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	focusedOtherY := 350.0
	visible := stationTargetRegions(t, "MOONGLOW CITY", 400, false, &focusedOtherY)
	focused := stationTargetRegions(t, "MOONGLOW CITY", 400, true, nil)
	locked := stationTargetRegions(t, "< MOONGLOW CITY >", 400, true, nil)
	caller := &stationTargetCaller{
		contactsStates: []string{"CONTACTS", "CONTACTS"},
		regions:        []json.RawMessage{visible, visible, focused, focused, locked, locked},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{"stationName": "MOONGLOW CITY"}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"DOWN", "SELECT"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("output=%s controls=%v", output, caller.controls)
		}
	}
	if !contains(string(output), `"result":"ACQUIRED"`) || !contains(string(output), `"navigationCount":1`) ||
		!contains(string(output), `"selectSent":true`) || !contains(string(output), `"selectAttemptCount":1`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSelectStationTargetRetriesOnceWhenFocusedRowDoesNotTransition(t *testing.T) {
	pkg, err := Load(selectStationTargetPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	focused := stationTargetRegions(t, "MOONGLOW CITY", 400, true, nil)
	locked := stationTargetRegions(t, "< MOONGLOW CITY >", 400, true, nil)
	caller := &stationTargetCaller{
		contactsStates: []string{"CONTACTS", "CONTACTS"},
		regions: []json.RawMessage{
			focused, focused,
			focused, focused, focused, focused,
			locked, locked,
		},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{"stationName": "MOONGLOW CITY"}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"SELECT", "SELECT"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
	if !contains(string(output), `"result":"ACQUIRED"`) || !contains(string(output), `"selectAttemptCount":2`) {
		t.Fatalf("output=%s", output)
	}
	retryActivity := false
	for index, eventType := range reporter.types {
		if eventType == ActivityEventType && contains(string(reporter.payloads[index]), "retrying once") {
			retryActivity = true
		}
	}
	if !retryActivity {
		t.Fatalf("retry activity missing: types=%v payloads=%s", reporter.types, reporter.payloads)
	}
}

func TestEliteSelectStationTargetFailsAfterTwoRejectedSelects(t *testing.T) {
	pkg, err := Load(selectStationTargetPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	focused := stationTargetRegions(t, "MOONGLOW CITY", 400, true, nil)
	regions := make([]json.RawMessage, 10)
	for index := range regions {
		regions[index] = focused
	}
	caller := &stationTargetCaller{
		contactsStates: []string{"CONTACTS", "CONTACTS"},
		regions:        regions,
	}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{"stationName": "MOONGLOW CITY"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "two bounded SELECT attempts") {
		t.Fatalf("error=%v", err)
	}
	wantControls := []string{"SELECT", "SELECT"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("controls=%v want=%v", caller.controls, wantControls)
	}
}

func TestEliteSelectStationTargetFailsWithoutNamedTargetAndNeverSelects(t *testing.T) {
	pkg, err := Load(selectStationTargetPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	unrelated := stationTargetRegions(t, "INTERNAL SECURITY SERVICE", 350, true, nil)
	caller := &stationTargetCaller{
		contactsStates: []string{"CONTACTS", "CONTACTS"},
		regions:        []json.RawMessage{unrelated, unrelated, unrelated, unrelated},
	}
	_, err = (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{"stationName": "MOONGLOW CITY"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "two consecutive known observations") || len(caller.controls) != 0 {
		t.Fatalf("error=%v controls=%v", err, caller.controls)
	}
}

func TestEliteDockAtStationCompletesAtVisualConfirmationHandoff(t *testing.T) {
	pkg, err := Load(dockAtStationPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &dockAtStationCaller{
		contactsStates: []string{"ABSENT", "ABSENT", "CONTACTS", "CONTACTS", "ABSENT", "ABSENT"},
		requestStates:  []string{"AVAILABLE", "FOCUSED", "DOCKING_ACTIVE", "DOCKING_ACTIVE"},
		flightStates:   []string{"AUTO_DOCK", "AUTO_DOCK", "AUTO_DOCK", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"},
		gearStates:     []string{"OFF", "OFF", "OFF", "OFF", "OFF", "OFF", "ON", "ON"},
		rangeState:     "ALLOWED",
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.rangeCalls != 4 || !contains(string(output), `"finalPhase":"VISUAL_CONFIRMATION_REQUIRED"`) ||
		!contains(string(output), `"admittedDistanceMeters":5390`) || !contains(string(output), `"visualConfirmed":false`) {
		t.Fatalf("output=%s rangeCalls=%d", output, caller.rangeCalls)
	}
	wantControls := []string{"FOCUS_LEFT_PANEL", "RIGHT", "SELECT", "FOCUS_LEFT_PANEL"}
	if len(caller.controls) != len(wantControls) {
		t.Fatalf("controls=%v", caller.controls)
	}
	for index := range wantControls {
		if caller.controls[index] != wantControls[index] {
			t.Fatalf("controls=%v", caller.controls)
		}
	}
	if len(reporter.types) == 0 || reporter.types[len(reporter.types)-1] != "action.dock-at-station.update" ||
		!contains(string(reporter.payloads[len(reporter.payloads)-1]), `"phase":"VISUAL_CONFIRMATION_REQUIRED"`) {
		t.Fatalf("events=%v payloads=%s", reporter.types, reporter.payloads)
	}
}

func TestEliteDockAtStationWaitsThroughTransientPanelUnknowns(t *testing.T) {
	pkg, err := Load(dockAtStationPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &dockAtStationCaller{
		contactsStates: []string{
			"ABSENT", "ABSENT",
			"UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "CONTACTS", "CONTACTS",
			"ABSENT", "ABSENT",
		},
		requestStates: []string{"AVAILABLE", "FOCUSED", "DOCKING_ACTIVE", "DOCKING_ACTIVE"},
		flightStates:  []string{"AUTO_DOCK", "AUTO_DOCK", "AUTO_DOCK", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"},
		gearStates:    []string{"OFF", "OFF", "OFF", "OFF", "OFF", "OFF", "ON", "ON"},
		rangeState:    "ALLOWED",
	}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"finalPhase":"VISUAL_CONFIRMATION_REQUIRED"`) || caller.contactsIndex != 10 {
		t.Fatalf("output=%s contactsIndex=%d", output, caller.contactsIndex)
	}
}

func TestEliteDockAtStationRecordsOneFailedObservationWithoutAdvancingGates(t *testing.T) {
	pkg, err := Load(dockAtStationPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &dockAtStationCaller{
		contactsStates:       []string{"ABSENT", "ABSENT", "CONTACTS", "CONTACTS", "ABSENT", "ABSENT"},
		requestStates:        []string{"AVAILABLE", "FOCUSED", "DOCKING_ACTIVE", "DOCKING_ACTIVE"},
		flightPromptFailures: 1,
		flightStates:         []string{"AUTO_DOCK", "AUTO_DOCK", "AUTO_DOCK", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"},
		gearStates:           []string{"OFF", "OFF", "OFF", "OFF", "OFF", "OFF", "ON", "ON"},
		rangeState:           "ALLOWED",
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"observationErrorCount":1`) {
		t.Fatalf("output=%s", output)
	}
	found := false
	for _, payload := range reporter.payloads {
		if contains(string(payload), `"phase":"OBSERVATION_ERROR"`) &&
			contains(string(payload), `"observationErrorCount":1`) &&
			contains(string(payload), `primary monitor capture size is invalid`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("observation failure was not emitted: %s", reporter.payloads)
	}
}

func TestEliteDockAtStationRejectsOCRDistanceOutlierBeforeTrendAdmission(t *testing.T) {
	pkg, err := Load(dockAtStationPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := &dockAtStationCaller{
		contactsStates: []string{"ABSENT", "ABSENT", "CONTACTS", "CONTACTS", "ABSENT", "ABSENT"},
		requestStates:  []string{"AVAILABLE", "FOCUSED", "DOCKING_ACTIVE", "DOCKING_ACTIVE"},
		flightStates:   []string{"AUTO_DOCK", "AUTO_DOCK", "AUTO_DOCK", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"},
		gearStates:     []string{"OFF", "OFF", "OFF", "OFF", "OFF", "OFF", "ON", "ON"},
		rangeStates:    []string{"DENIED", "DENIED", "DENIED", "DENIED", "DENIED", "ALLOWED", "ALLOWED"},
		rangeDistances: []float64{8840, 84000, 8720, 8500, 7500, 7400, 7300},
	}
	reporter := &fixtureReporter{}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, reporter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if caller.rangeCalls != 7 || !contains(string(output), `"rangeWaitSamples":7`) ||
		!contains(string(output), `"admittedDistanceMeters":7300`) ||
		!contains(string(output), `"rangeTrendSamples":5`) ||
		!contains(string(output), `"rangeOutlierCount":2`) {
		t.Fatalf("output=%s rangeCalls=%d", output, caller.rangeCalls)
	}
	waitEvents := 0
	for _, payload := range reporter.payloads {
		if contains(string(payload), `"phase":"RANGE_WAIT"`) {
			waitEvents++
		}
	}
	if waitEvents != 7 {
		t.Fatalf("range wait events=%d payloads=%s", waitEvents, reporter.payloads)
	}
}

func TestEliteDockAtStationRefocusesAndRetriesDroppedSelect(t *testing.T) {
	pkg, err := Load(dockAtStationPackageRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	requestStates := []string{"AVAILABLE", "FOCUSED"}
	for index := 0; index < 12; index++ {
		requestStates = append(requestStates, "AVAILABLE")
	}
	requestStates = append(requestStates, "FOCUSED", "DOCKING_ACTIVE", "DOCKING_ACTIVE")
	caller := &dockAtStationCaller{
		contactsStates: []string{"ABSENT", "ABSENT", "CONTACTS", "CONTACTS", "ABSENT", "ABSENT"},
		requestStates:  requestStates,
		flightStates:   []string{"AUTO_DOCK", "AUTO_DOCK", "AUTO_DOCK", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN", "UNKNOWN"},
		gearStates:     []string{"OFF", "OFF", "OFF", "OFF", "OFF", "OFF", "ON", "ON"},
		rangeState:     "ALLOWED",
	}
	output, err := (Runner{Sleep: func(context.Context, time.Duration) error { return nil }}).Run(
		context.Background(), pkg, map[string]any{}, caller, &fixtureReporter{},
	)
	if err != nil || !contains(string(output), `"finalPhase":"VISUAL_CONFIRMATION_REQUIRED"`) {
		t.Fatalf("output=%s error=%v", output, err)
	}
	selects := 0
	rights := 0
	for _, control := range caller.controls {
		if control == "SELECT" {
			selects++
		}
		if control == "RIGHT" {
			rights++
		}
	}
	if selects != 2 || rights != 2 {
		t.Fatalf("controls=%v", caller.controls)
	}
}

func writeFixturePackage(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.star":          script,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  `{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"type":"boolean"}}}`,
		"output.schema.json": `{"type":"object","additionalProperties":false,"required":["done","sequence"],"properties":{"done":{"const":true},"sequence":{"type":"integer","minimum":0}}}`,
		"event.schema.json":  `{"type":"object","additionalProperties":false,"required":["type","payload"],"properties":{"type":{"const":"action.phase.changed"},"payload":{"type":"object","additionalProperties":false,"required":["phase","massLock"],"properties":{"phase":{"const":"DONE"},"massLock":{"const":"OFF"}}}}}`,
	}
	manifest := `{
  "schemaVersion":1,
  "version":1,
  "title":"Fixture streaming Action",
  "entrypoint":"main.star",
  "taskDocument":"TASK.md",
  "inputSchema":"input.schema.json",
  "outputSchema":"output.schema.json",
  "eventSchema":"event.schema.json",
  "files":["main.star","TASK.md","input.schema.json","output.schema.json","event.schema.json"],
  "limits":{"maxSteps":100000,"maxOutputBytes":4096,"maxEventBytes":4096,"maxSleepMs":10000}
}`
	files[ManifestName] = manifest
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func contains(value, expected string) bool {
	for index := 0; index+len(expected) <= len(value); index++ {
		if value[index:index+len(expected)] == expected {
			return true
		}
	}
	return false
}
