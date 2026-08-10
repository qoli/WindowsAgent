package pointeraction

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
)

type recordingPointerDriver struct {
	request windowsinput.PointerClickRequest
}

func (d *recordingPointerDriver) ClickReference(_ context.Context, request windowsinput.PointerClickRequest) (windowsinput.PointerEvidence, error) {
	d.request = request
	return windowsinput.PointerEvidence{Backend: windowsinput.BackendSendInputPointer, ReferenceX: request.ReferenceX, ReferenceY: request.ReferenceY,
		ScreenX: 1920, ScreenY: 1080, ScreenWidth: 3840, ScreenHeight: 2160,
		ViewportWidth: 3840, ViewportHeight: 2160}, nil
}

func TestControllerMapsValidatedReferenceClickAndReportsEvidence(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "pointer-click"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	driver := &recordingPointerDriver{}
	controller, err := NewController(driver, func() (foreground.Info, error) {
		return foreground.Info{ProcessID: 7, ExecutableName: "EliteDangerous64.exe"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := controller.Run(context.Background(), pkg, map[string]any{"x": float64(960), "y": float64(540)}, "EliteDangerous64.exe")
	if err != nil {
		t.Fatal(err)
	}
	if driver.request.ReferenceX != 960 || driver.request.ReferenceY != 540 {
		t.Fatalf("request=%+v", driver.request)
	}
	if driver.request.Hold.Milliseconds() != 40 {
		t.Fatalf("hold=%s", driver.request.Hold)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result["backend"] != windowsinput.BackendSendInputPointer || result["screenX"] != float64(1920) {
		t.Fatalf("result=%s", raw)
	}
}

func TestPackageRejectsOutOfRangeCoordinates(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "pointer-click"))
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := pkg.ValidateInput(map[string]any{"x": 1920.0, "y": 0.0}); err == nil {
		t.Fatal("out-of-range x accepted")
	}
}
