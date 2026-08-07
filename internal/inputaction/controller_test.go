package inputaction

import (
	"context"
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
