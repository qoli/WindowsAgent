package inputaction

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

type recordingDriver struct {
	requests []windowsinput.PressRequest
	downs    []windowsinput.KeyRequest
	ups      []windowsinput.KeyRequest
	err      error
}

func (d *recordingDriver) Press(_ context.Context, request windowsinput.PressRequest) (windowsinput.Evidence, error) {
	d.requests = append(d.requests, request)
	if d.err != nil {
		return windowsinput.Evidence{}, d.err
	}
	return windowsinput.Evidence{
		Backend: windowsinput.BackendSendInputScanCode, Key: request.Key,
		ScanCode: 0x41, Extended: false, HoldMS: request.Hold.Milliseconds(),
	}, nil
}

func (d *recordingDriver) KeyDown(_ context.Context, request windowsinput.KeyRequest) (windowsinput.Evidence, error) {
	d.downs = append(d.downs, request)
	if d.err != nil {
		return windowsinput.Evidence{}, d.err
	}
	return windowsinput.Evidence{Backend: windowsinput.BackendSendInputScanCode, Key: request.Key, ScanCode: 0x41}, nil
}

func (d *recordingDriver) KeyUp(_ context.Context, request windowsinput.KeyRequest) (windowsinput.Evidence, error) {
	d.ups = append(d.ups, request)
	if d.err != nil {
		return windowsinput.Evidence{}, d.err
	}
	return windowsinput.Evidence{Backend: windowsinput.BackendSendInputScanCode, Key: request.Key, ScanCode: 0x41}, nil
}

func TestControllerResolvesCurrentKeyboardBindingAndPressesIt(t *testing.T) {
	pkg := writeInputPackage(t, Selector{InputField: "percent"}, map[string]Binding{
		"0": {Control: "SetSpeedZero"}, "100": {Control: "SetSpeed100"},
	})
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <SetSpeedZero><Primary Device="Keyboard" Key="Key_X"/><Secondary Device="{NoDevice}" Key=""/></SetSpeedZero>
  <SetSpeed100><Primary Device="{NoDevice}" Key=""/><Secondary Device="Keyboard" Key="Key_F7"/></SetSpeed100>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"percent": int64(100)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_F7" || driver.requests[0].Hold != 40*time.Millisecond ||
		!strings.Contains(string(output), `"key":"Key_F7"`) || !strings.Contains(string(output), `"activePreset":"ControlPadKeyboard"`) ||
		!strings.Contains(string(output), `"backend":"sendinput-scancode"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestControllerStartsRenewsAndStopsKeyHoldLease(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <PitchUpButton><Primary Device="Keyboard" Key="Key_UpArrow"/><Secondary Device="{NoDevice}" Key=""/></PitchUpButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	started, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "PITCH_UP"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var startResult map[string]any
	if err := json.Unmarshal(started, &startResult); err != nil {
		t.Fatal(err)
	}
	leaseID, _ := startResult["leaseId"].(string)
	if !strings.HasPrefix(leaseID, "key_") || len(driver.downs) != 1 || driver.downs[0].Key != "Key_UpArrow" {
		t.Fatalf("started=%s downs=%v", started, driver.downs)
	}
	renewed, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "RENEW", "control": "PITCH_UP", "leaseId": leaseID}, "EliteDangerous64.exe")
	if err != nil || !strings.Contains(string(renewed), `"operation":"RENEW"`) {
		t.Fatalf("renewed=%s error=%v", renewed, err)
	}
	controller.expireLease(leaseID, 1)
	if len(driver.ups) != 0 {
		t.Fatalf("stale expiry released renewed lease: ups=%v", driver.ups)
	}
	stopped, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "PITCH_UP", "leaseId": leaseID}, "EliteDangerous64.exe")
	if err != nil || !strings.Contains(string(stopped), `"leaseState":"RELEASED"`) || !strings.Contains(string(stopped), `"releaseReason":"EXPLICIT"`) {
		t.Fatalf("stopped=%s error=%v", stopped, err)
	}
	if len(driver.ups) != 1 || driver.ups[0].Key != "Key_UpArrow" {
		t.Fatalf("ups=%v", driver.ups)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "PITCH_UP", "leaseId": leaseID}, "EliteDangerous64.exe"); err != nil || len(driver.ups) != 1 {
		t.Fatalf("idempotent stop error=%v ups=%v", err, driver.ups)
	}
}

func TestControllerStartsAndStopsCompoundKeyHoldLease(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-vector-hold"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <PitchUpButton><Primary Device="Keyboard" Key="Key_UpArrow"/></PitchUpButton>
  <YawLeftButton><Primary Device="Keyboard" Key="Key_A"/></YawLeftButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	started, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "PITCH_UP_YAW_LEFT"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(started, &result); err != nil {
		t.Fatal(err)
	}
	leaseID := result["leaseId"].(string)
	if len(driver.downs) != 2 || driver.downs[0].Key != "Key_UpArrow" || driver.downs[1].Key != "Key_A" ||
		!strings.Contains(string(started), `"controls":["PitchUpButton","YawLeftButton"]`) ||
		!strings.Contains(string(started), `"keys":["Key_UpArrow","Key_A"]`) {
		t.Fatalf("started=%s downs=%v", started, driver.downs)
	}
	stopped, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "PITCH_UP_YAW_LEFT", "leaseId": leaseID}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.ups) != 2 || driver.ups[0].Key != "Key_A" || driver.ups[1].Key != "Key_UpArrow" ||
		!strings.Contains(string(stopped), `"leaseState":"RELEASED"`) {
		t.Fatalf("stopped=%s ups=%v", stopped, driver.ups)
	}
}

func TestControllerExpiresKeyHoldLeaseAndReportsExpiryOnStop(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <YawLeftButton><Primary Device="Keyboard" Key="Key_A"/><Secondary Device="{NoDevice}" Key=""/></YawLeftButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	started, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "YAW_LEFT"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(started, &result); err != nil {
		t.Fatal(err)
	}
	leaseID := result["leaseId"].(string)
	controller.expireLease(leaseID, 1)
	stopped, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "YAW_LEFT", "leaseId": leaseID}, "EliteDangerous64.exe")
	if err != nil || !strings.Contains(string(stopped), `"releaseReason":"EXPIRED"`) || len(driver.ups) != 1 {
		t.Fatalf("stopped=%s error=%v ups=%v", stopped, err, driver.ups)
	}
}

func TestControllerStopsOlderExpiredLeaseAfterNewLeaseStarts(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <YawLeftButton><Primary Device="Keyboard" Key="Key_A"/></YawLeftButton>
  <YawRightButton><Primary Device="Keyboard" Key="Key_D"/></YawRightButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	first, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "YAW_LEFT"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var firstResult map[string]any
	if err := json.Unmarshal(first, &firstResult); err != nil {
		t.Fatal(err)
	}
	firstLeaseID := firstResult["leaseId"].(string)
	controller.expireLease(firstLeaseID, 1)

	second, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "YAW_RIGHT"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	var secondResult map[string]any
	if err := json.Unmarshal(second, &secondResult); err != nil {
		t.Fatal(err)
	}
	secondLeaseID := secondResult["leaseId"].(string)

	stopped, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "YAW_LEFT", "leaseId": firstLeaseID}, "EliteDangerous64.exe")
	if err != nil || !strings.Contains(string(stopped), `"releaseReason":"EXPIRED"`) {
		t.Fatalf("stopped=%s error=%v", stopped, err)
	}
	if len(driver.ups) != 1 || driver.ups[0].Key != "Key_A" {
		t.Fatalf("stopping older expired lease altered active key: ups=%v", driver.ups)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "YAW_RIGHT", "leaseId": secondLeaseID}, "EliteDangerous64.exe"); err != nil {
		t.Fatal(err)
	}
}

func TestControllerRejectsUnknownLeaseWhileAnotherLeaseIsActive(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <YawLeftButton><Primary Device="Keyboard" Key="Key_A"/></YawLeftButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "START", "control": "YAW_LEFT"}, "EliteDangerous64.exe"); err != nil {
		t.Fatal(err)
	}
	unknownLeaseID := "key_00000000000000000000000000000000"
	if _, err := controller.Run(context.Background(), pkg, map[string]any{"operation": "STOP", "control": "YAW_LEFT", "leaseId": unknownLeaseID}, "EliteDangerous64.exe"); err == nil {
		t.Fatal("unknown lease unexpectedly stopped active input")
	}
	if len(driver.ups) != 0 {
		t.Fatalf("unknown lease released active key: ups=%v", driver.ups)
	}
}

func TestControllerAllowsDistinctPressDuringKeyHoldLease(t *testing.T) {
	holdRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	holdPackage, err := Load(holdRoot)
	if err != nil {
		t.Fatal(err)
	}
	pressPackage := writeInputPackage(t, Selector{InputField: "percent"}, map[string]Binding{
		"0": {Control: "SetSpeedZero"},
	})
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <PitchUpButton><Primary Device="Keyboard" Key="Key_UpArrow"/></PitchUpButton>
  <SetSpeedZero><Primary Device="Keyboard" Key="Key_X"/></SetSpeedZero>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), holdPackage, map[string]any{"operation": "START", "control": "PITCH_UP"}, "EliteDangerous64.exe"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pressPackage, map[string]any{"percent": int64(0)}, "EliteDangerous64.exe"); err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_X" || len(driver.ups) != 0 {
		t.Fatalf("requests=%v ups=%v", driver.requests, driver.ups)
	}
}

func TestControllerRejectsConflictingPressDuringKeyHoldLease(t *testing.T) {
	holdRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-hold"))
	if err != nil {
		t.Fatal(err)
	}
	holdPackage, err := Load(holdRoot)
	if err != nil {
		t.Fatal(err)
	}
	pressPackage := writeInputPackage(t, Selector{InputField: "percent"}, map[string]Binding{
		"0": {Control: "SetSpeedZero"},
	})
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <PitchUpButton><Primary Device="Keyboard" Key="Key_UpArrow"/></PitchUpButton>
  <SetSpeedZero><Primary Device="Keyboard" Key="Key_UpArrow"/></SetSpeedZero>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), holdPackage, map[string]any{"operation": "START", "control": "PITCH_UP"}, "EliteDangerous64.exe"); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pressPackage, map[string]any{"percent": int64(0)}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "conflicts with active key hold lease") {
		t.Fatalf("conflicting press error=%v", err)
	}
	if len(driver.requests) != 0 || len(driver.ups) != 0 {
		t.Fatalf("conflicting press mutated input: requests=%v ups=%v", driver.requests, driver.ups)
	}
}

func TestControllerUsesValidatedDynamicHoldOverride(t *testing.T) {
	pkg := writeInputPackage(t, Selector{InputField: "percent"}, map[string]Binding{
		"0": {Control: "SetSpeedZero"},
	})
	pkg.Manifest.Gesture = Gesture{
		Type: "press", HoldMS: 40, HoldMSInputField: "holdMs", MinHoldMS: 40, MaxHoldMS: 1000,
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <SetSpeedZero><Primary Device="Keyboard" Key="Key_X"/></SetSpeedZero>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{"percent": int64(0), "holdMs": int64(250)}, "EliteDangerous64.exe"); err == nil {
		// The fixture package schema intentionally rejects undeclared holdMs.
		t.Fatal("dynamic hold unexpectedly bypassed input schema")
	}
	if len(driver.requests) != 0 {
		t.Fatalf("unexpected requests=%v", driver.requests)
	}
	if got, err := resolveHoldMS(pkg.Manifest.Gesture, map[string]any{"holdMs": int64(250)}); err != nil || got != 250 {
		t.Fatalf("resolveHoldMS=%d,%v", got, err)
	}
	for _, value := range []any{int64(39), int64(1001), 40.5, "250"} {
		if _, err := resolveHoldMS(pkg.Manifest.Gesture, map[string]any{"holdMs": value}); err == nil {
			t.Fatalf("resolveHoldMS(%#v) unexpectedly succeeded", value)
		}
	}
}

func TestEliteSetThrottleResolvesMinus100Binding(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "set-throttle"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <SetSpeedMinus100><Primary Device="Keyboard" Key="Key_F8"/><Secondary Device="{NoDevice}" Key=""/></SetSpeedMinus100>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"percent": int64(-100)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_F8" ||
		!strings.Contains(string(output), `"selection":"-100"`) ||
		!strings.Contains(string(output), `"control":"SetSpeedMinus100"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestEliteSetThrottleResolves75Binding(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "set-throttle"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <SetSpeed75><Primary Device="Keyboard" Key="Key_F9"/><Secondary Device="{NoDevice}" Key=""/></SetSpeed75>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"percent": int64(75)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_F9" ||
		!strings.Contains(string(output), `"selection":"75"`) ||
		!strings.Contains(string(output), `"control":"SetSpeed75"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestEliteSupercruiseControlRequiresDedicatedBinding(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "supercruise-control"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <Supercruise><Primary Device="Keyboard" Key="Key_J"/><Secondary Device="{NoDevice}" Key=""/></Supercruise>
  <HyperSuperCombination><Primary Device="Keyboard" Key="Key_K"/><Secondary Device="{NoDevice}" Key=""/></HyperSuperCombination>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"command": "TOGGLE"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_J" ||
		!strings.Contains(string(output), `"control":"Supercruise"`) || strings.Contains(string(output), "HyperSuperCombination") {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestEliteUIControlResolvesFocusLeftPanel(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ui-control"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `
<Root PresetName="ControlPadKeyboard">
  <FocusLeftPanel>
    <Primary Device="28DE11FF" Key="GamePad_DPadLeft" />
    <Secondary Device="Keyboard" Key="Key_1" />
  </FocusLeftPanel>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"control": "FOCUS_LEFT_PANEL"}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_1" ||
		!strings.Contains(string(output), `"selection":"FOCUS_LEFT_PANEL"`) ||
		!strings.Contains(string(output), `"control":"FocusLeftPanel"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestEliteUIControlResolvesDedicatedPanelCycleControls(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ui-control"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <CycleNextPanel><Primary Device="Keyboard" Key="Key_E"/><Secondary Device="{NoDevice}" Key=""/></CycleNextPanel>
  <CyclePreviousPanel><Primary Device="Keyboard" Key="Key_Q"/><Secondary Device="{NoDevice}" Key=""/></CyclePreviousPanel>
</Root>`)
	for _, test := range []struct{ selection, control, key string }{
		{"NEXT_PANEL", "CycleNextPanel", "Key_E"},
		{"PREVIOUS_PANEL", "CyclePreviousPanel", "Key_Q"},
	} {
		t.Run(test.selection, func(t *testing.T) {
			driver := &recordingDriver{}
			controller, err := NewController(bindingsRoot, driver, fixtureForeground)
			if err != nil {
				t.Fatal(err)
			}
			output, err := controller.Run(context.Background(), pkg, map[string]any{"control": test.selection}, "EliteDangerous64.exe")
			if err != nil {
				t.Fatal(err)
			}
			if len(driver.requests) != 1 || driver.requests[0].Key != test.key ||
				!strings.Contains(string(output), `"control":"`+test.control+`"`) {
				t.Fatalf("requests=%v output=%s", driver.requests, output)
			}
		})
	}
}

func TestEliteShipAttitudeControlResolvesFrontierArrowAndDynamicHold(t *testing.T) {
	packageRoot, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-attitude-control"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	bindingsRoot := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard">
  <PitchUpButton><Primary Device="Keyboard" Key="Key_UpArrow"/></PitchUpButton>
</Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(bindingsRoot, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"control": "PITCH_UP", "holdMs": int64(250)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_UpArrow" || driver.requests[0].Hold != 250*time.Millisecond ||
		!strings.Contains(string(output), `"selection":"PITCH_UP"`) || !strings.Contains(string(output), `"holdMs":250`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestControllerRejectsMissingUnsupportedOrAmbiguousBindings(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	for _, test := range []struct {
		name, xml, want string
	}{
		{"missing", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="{NoDevice}" Key=""/><Secondary Device="{NoDevice}" Key=""/></UI_Select></Root>`, "no Keyboard binding"},
		{"unsupported", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="Keyboard" Key="Key_PrintScreen"/><Secondary Device="{NoDevice}" Key=""/></UI_Select></Root>`, "unsupported Windows input key"},
		{"ambiguous", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/><Secondary Device="Keyboard" Key="Key_X"/></UI_Select></Root>`, "ambiguous Keyboard bindings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeBindings(t, "ControlPadKeyboard", test.xml)
			controller, err := NewController(root, &recordingDriver{}, fixtureForeground)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}

func TestControllerResolvesActivePresetByRootDeclaration(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	root := writeBindings(t, "Custom", `<Root PresetName="Custom"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/></UI_Select></Root>`)
	driver := &recordingDriver{}
	controller, err := NewController(root, driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || !strings.Contains(string(output), `"bindingFile":"Custom.4.2.binds"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestControllerRunsLiteralKeyPackageWithoutFrontierResolution(t *testing.T) {
	pkg := writeInputPackageForSource(t, BindingSource{Type: BindingSourceLiteral}, Selector{Constant: "open"}, map[string]Binding{
		"open": {Key: "Key_F12"},
	})
	driver := &recordingDriver{}
	controller, err := NewController("", driver, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(driver.requests) != 1 || driver.requests[0].Key != "Key_F12" || !strings.Contains(string(output), `"bindingSource":"literal-key-v1"`) {
		t.Fatalf("requests=%v output=%s", driver.requests, output)
	}
}

func TestControllerRequiresFrontierConfigurationOnlyForFrontierSource(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	controller, err := NewController("", &recordingDriver{}, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "Frontier binding source is not configured") {
		t.Fatalf("error=%v", err)
	}
}

func TestControllerRejectsMissingActivePresetOrForegroundDrift(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	root := writeBindings(t, "Custom", `<Root PresetName="Different"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/></UI_Select></Root>`)
	controller, err := NewController(root, &recordingDriver{}, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "no .binds file declares active") {
		t.Fatalf("preset error=%v", err)
	}
	controller, err = NewController(root, &recordingDriver{}, func() (foreground.Info, error) {
		return foreground.Info{ProcessID: 4, ExecutableName: "Other.exe", ExecutablePath: `C:\Other.exe`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "expected owning Rule") {
		t.Fatalf("foreground error=%v", err)
	}
}

func TestControllerRejectsForegroundChangeBeforeInjection(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	root := writeBindings(t, "ControlPadKeyboard", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/></UI_Select></Root>`)
	driver := &recordingDriver{}
	calls := 0
	controller, err := NewController(root, driver, func() (foreground.Info, error) {
		calls++
		if calls == 1 {
			return fixtureForeground()
		}
		return foreground.Info{ProcessID: 7, ExecutableName: "Other.exe", ExecutablePath: `C:\Other.exe`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "foreground process changed") {
		t.Fatalf("error=%v", err)
	}
	if len(driver.requests) != 0 {
		t.Fatalf("unexpected requests=%v", driver.requests)
	}
}

func fixtureForeground() (foreground.Info, error) {
	return foreground.Info{ProcessID: 42, ExecutableName: "EliteDangerous64.exe", ExecutablePath: `D:\EliteDangerous64.exe`}, nil
}

func writeBindings(t *testing.T, activePreset, document string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, activePresetFilename), []byte(activePreset+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, activePreset+".4.2.binds"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeInputPackage(t *testing.T, selector Selector, bindings map[string]Binding) *Package {
	return writeInputPackageForSource(t, BindingSource{Type: BindingSourceFrontier}, selector, bindings)
}

func writeInputPackageForSource(t *testing.T, source BindingSource, selector Selector, bindings map[string]Binding) *Package {
	t.Helper()
	root := t.TempDir()
	selectorJSON := `"constant":"` + selector.Constant + `"`
	if selector.InputField != "" {
		selectorJSON = `"inputField":"` + selector.InputField + `"`
	}
	bindingParts := make([]string, 0, len(bindings))
	for name, binding := range bindings {
		if binding.Control != "" {
			bindingParts = append(bindingParts, `"`+name+`":{"control":"`+binding.Control+`"}`)
		} else {
			bindingParts = append(bindingParts, `"`+name+`":{"key":"`+binding.Key+`"}`)
		}
	}
	manifest := `{"schemaVersion":1,"version":1,"title":"Fixture","inputSchema":"input.schema.json","outputSchema":"output.schema.json","taskDocument":"TASK.md","bindingSource":{"type":"` + source.Type + `"},"gesture":{"type":"press","holdMs":40},"selector":{` + selectorJSON + `},"bindings":{` + strings.Join(bindingParts, ",") + `},"files":["TASK.md","input.schema.json","output.schema.json"]}`
	inputSchema := `{"type":"object","additionalProperties":false}`
	if selector.InputField != "" {
		inputSchema = `{"type":"object","required":["percent"],"properties":{"percent":{"enum":[0,100]}},"additionalProperties":false}`
	}
	files := map[string]string{
		ManifestName:         manifest,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  inputSchema,
		"output.schema.json": `{"type":"object","additionalProperties":true,"required":["schemaVersion","selection","key","bindingSource","backend","scanCode","extended","holdMs"],"properties":{"schemaVersion":{"const":1},"selection":{"type":"string"},"key":{"type":"string"},"bindingSource":{"type":"string"},"backend":{"const":"sendinput-scancode"},"scanCode":{"type":"integer"},"extended":{"type":"boolean"},"holdMs":{"const":40}}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}
