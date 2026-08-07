package inputaction

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/foreground"
)

type recordingSender struct {
	keys []uint16
	err  error
}

func (s *recordingSender) Press(_ context.Context, key uint16) error {
	s.keys = append(s.keys, key)
	return s.err
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
	sender := &recordingSender{}
	controller, err := NewController(bindingsRoot, sender, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{"percent": int64(100)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.keys) != 1 || sender.keys[0] != 0x76 || !strings.Contains(string(output), `"key":"Key_F7"`) || !strings.Contains(string(output), `"activePreset":"ControlPadKeyboard"`) {
		t.Fatalf("keys=%v output=%s", sender.keys, output)
	}
}

func TestControllerRejectsMissingUnsupportedOrAmbiguousBindings(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	for _, test := range []struct {
		name, xml, want string
	}{
		{"missing", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="{NoDevice}" Key=""/><Secondary Device="{NoDevice}" Key=""/></UI_Select></Root>`, "no Keyboard binding"},
		{"unsupported", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="Keyboard" Key="Key_F6"/><Secondary Device="{NoDevice}" Key=""/></UI_Select></Root>`, "unsupported Frontier keyboard key"},
		{"ambiguous", `<Root PresetName="ControlPadKeyboard"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/><Secondary Device="Keyboard" Key="Key_X"/></UI_Select></Root>`, "ambiguous Keyboard bindings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeBindings(t, "ControlPadKeyboard", test.xml)
			controller, err := NewController(root, &recordingSender{}, fixtureForeground)
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
	sender := &recordingSender{}
	controller, err := NewController(root, sender, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	output, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.keys) != 1 || !strings.Contains(string(output), `"bindingFile":"Custom.4.2.binds"`) {
		t.Fatalf("keys=%v output=%s", sender.keys, output)
	}
}

func TestControllerRejectsMissingActivePresetOrForegroundDrift(t *testing.T) {
	pkg := writeInputPackage(t, Selector{Constant: "select"}, map[string]Binding{"select": {Control: "UI_Select"}})
	root := writeBindings(t, "Custom", `<Root PresetName="Different"><UI_Select><Primary Device="Keyboard" Key="Key_Space"/></UI_Select></Root>`)
	controller, err := NewController(root, &recordingSender{}, fixtureForeground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(context.Background(), pkg, map[string]any{}, "EliteDangerous64.exe"); err == nil || !strings.Contains(err.Error(), "no .binds file declares active") {
		t.Fatalf("preset error=%v", err)
	}
	controller, err = NewController(root, &recordingSender{}, func() (foreground.Info, error) {
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
	sender := &recordingSender{}
	calls := 0
	controller, err := NewController(root, sender, func() (foreground.Info, error) {
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
	if len(sender.keys) != 0 {
		t.Fatalf("unexpected keys=%v", sender.keys)
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
	t.Helper()
	root := t.TempDir()
	selectorJSON := `"constant":"` + selector.Constant + `"`
	if selector.InputField != "" {
		selectorJSON = `"inputField":"` + selector.InputField + `"`
	}
	bindingParts := make([]string, 0, len(bindings))
	for name, binding := range bindings {
		bindingParts = append(bindingParts, `"`+name+`":{"control":"`+binding.Control+`"}`)
	}
	manifest := `{"schemaVersion":1,"version":1,"title":"Fixture","inputSchema":"input.schema.json","outputSchema":"output.schema.json","taskDocument":"TASK.md","selector":{` + selectorJSON + `},"bindings":{` + strings.Join(bindingParts, ",") + `},"files":["TASK.md","input.schema.json","output.schema.json"]}`
	inputSchema := `{"type":"object","additionalProperties":false}`
	if selector.InputField != "" {
		inputSchema = `{"type":"object","required":["percent"],"properties":{"percent":{"enum":[0,100]}},"additionalProperties":false}`
	}
	files := map[string]string{
		ManifestName:         manifest,
		"TASK.md":            "# Fixture\n",
		"input.schema.json":  inputSchema,
		"output.schema.json": `{"type":"object","additionalProperties":false,"required":["schemaVersion","selection","control","key","activePreset","bindingFile"],"properties":{"schemaVersion":{"const":1},"selection":{"type":"string"},"control":{"type":"string"},"key":{"type":"string"},"activePreset":{"type":"string"},"bindingFile":{"type":"string"}}}`,
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
