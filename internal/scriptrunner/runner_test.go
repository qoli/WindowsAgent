package scriptrunner

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

const crimsonImageSHA256 = "d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f"

type fixtureBroker struct {
	calls         []string
	observerCalls []fixtureObserverCall
}

type fixtureObserverCall struct {
	namespace string
	operation string
	arguments map[string]any
}

type compassBroker struct {
	pixels []any
	calls  []fixtureObserverCall
}

type wideOcclusionBroker struct {
	strips map[int64][]any
	calls  []fixtureObserverCall
}

type shipStatusBroker struct {
	pixels []any
	calls  []fixtureObserverCall
}

type leftPanelTabBroker struct {
	pixels map[string][]any
	calls  []fixtureObserverCall
}

type stationServiceFocusBroker struct {
	pixels []any
	calls  []fixtureObserverCall
}

func (b *compassBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected compass blob path request")
}

func (b *compassBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected compass native record")
}

func (b *compassBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, fixtureObserverCall{namespace: namespace, operation: operation, arguments: arguments})
	x, _ := arguments["x"].(int64)
	y, _ := arguments["y"].(int64)
	w, _ := arguments["w"].(int64)
	h, _ := arguments["h"].(int64)
	return map[string]any{
		"sampling": "reference",
		"coordinateSpace": map[string]any{
			"width": int64(1920), "height": int64(1080), "fit": "centered-16:9",
		},
		"frame": map[string]any{
			"width": int64(3840), "height": int64(2160),
			"capturedAt": "2026-08-07T01:02:03Z",
			"foreground": map[string]any{"processId": int64(7), "executableName": "EliteDangerous64.exe"},
		},
		"viewport": map[string]any{
			"left": int64(0), "top": int64(0), "width": int64(3840), "height": int64(2160),
		},
		"region": map[string]any{
			"x": x, "y": y, "w": w, "h": h,
		},
		"physicalRegion": map[string]any{
			"left": x * 2, "top": y * 2, "width": w * 2, "height": h * 2,
		},
		"image": map[string]any{
			"width": w, "height": h,
			"encoding": "rgb24-packed", "pixels": b.pixels,
		},
	}, nil
}

func (b *wideOcclusionBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected wide-occlusion blob path request")
}

func (b *wideOcclusionBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected wide-occlusion native record")
}

func (b *wideOcclusionBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, fixtureObserverCall{namespace: namespace, operation: operation, arguments: arguments})
	x := arguments["x"].(int64)
	y := arguments["y"].(int64)
	w := arguments["w"].(int64)
	h := arguments["h"].(int64)
	return map[string]any{
		"sampling":        "reference",
		"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
		"frame":           map[string]any{"width": int64(3840), "height": int64(2160), "capturedAt": "2026-08-10T12:00:00Z"},
		"region":          map[string]any{"x": x, "y": y, "w": w, "h": h},
		"image":           map[string]any{"width": w, "height": h, "encoding": "rgb24-packed", "pixels": b.strips[y]},
	}, nil
}

func (b *leftPanelTabBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected left-panel-tab blob path request")
}

func (b *leftPanelTabBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected left-panel-tab native record")
}

func (b *leftPanelTabBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, fixtureObserverCall{namespace: namespace, operation: operation, arguments: arguments})
	x, xOK := arguments["x"].(int64)
	y, yOK := arguments["y"].(int64)
	w, wOK := arguments["w"].(int64)
	h, hOK := arguments["h"].(int64)
	if !xOK || !yOK || !wOK || !hOK {
		return nil, errors.New("tab sample coordinates are invalid")
	}
	pixels, exists := b.pixels[fmt.Sprintf("%d,%d", x, y)]
	if !exists {
		return nil, fmt.Errorf("unexpected tab sample at %d,%d", x, y)
	}
	return map[string]any{
		"sampling":        "reference",
		"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
		"frame":           map[string]any{"width": int64(3840), "height": int64(2160), "capturedAt": "2026-08-08T01:02:03Z"},
		"region":          map[string]any{"x": x, "y": y, "w": w, "h": h},
		"physicalRegion":  map[string]any{"left": x * 2, "top": y * 2, "width": w * 2, "height": h * 2},
		"image":           map[string]any{"width": w, "height": h, "encoding": "rgb24-packed", "pixels": pixels},
	}, nil
}

func (b *stationServiceFocusBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected station-service-focus blob path request")
}

func (b *stationServiceFocusBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected station-service-focus native record")
}

func (b *stationServiceFocusBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, fixtureObserverCall{namespace: namespace, operation: operation, arguments: arguments})
	x := arguments["x"].(int64)
	y := arguments["y"].(int64)
	w := arguments["w"].(int64)
	h := arguments["h"].(int64)
	return map[string]any{
		"sampling":        "reference",
		"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
		"frame":           map[string]any{"width": int64(3840), "height": int64(2160), "capturedAt": "2026-08-11T04:49:43Z"},
		"region":          map[string]any{"x": x, "y": y, "w": w, "h": h},
		"physicalRegion":  map[string]any{"left": x * 2, "top": y * 2, "width": w * 2, "height": h * 2},
		"image":           map[string]any{"width": w, "height": h, "encoding": "rgb24-packed", "pixels": b.pixels},
	}, nil
}

func compassPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "compass"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func hyperspaceTargetOcclusionPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "hyperspace-target-occlusion"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func cockpitHUDPresencePackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "cockpit-hud-presence"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func (b *shipStatusBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected ship-status blob path request")
}

func (b *shipStatusBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected ship-status native record")
}

func (b *shipStatusBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, fixtureObserverCall{namespace: namespace, operation: operation, arguments: arguments})
	return map[string]any{
		"sampling": "reference",
		"coordinateSpace": map[string]any{
			"width": int64(1920), "height": int64(1080), "fit": "centered-16:9",
		},
		"frame": map[string]any{
			"width": int64(3840), "height": int64(2160),
			"capturedAt": "2026-08-07T01:02:03Z",
			"foreground": map[string]any{"processId": int64(7), "executableName": "EliteDangerous64.exe"},
		},
		"viewport": map[string]any{
			"left": int64(0), "top": int64(0), "width": int64(3840), "height": int64(2160),
		},
		"region": map[string]any{
			"x": int64(1650), "y": int64(900), "w": int64(270), "h": int64(180),
		},
		"physicalRegion": map[string]any{
			"left": int64(3300), "top": int64(1800), "width": int64(540), "height": int64(360),
		},
		"image": map[string]any{
			"width": int64(270), "height": int64(180),
			"encoding": "rgb24-packed", "pixels": b.pixels,
		},
	}, nil
}

func shipStatusPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-status-classifier"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func shipSpeedPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-speed-classifier"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func shipHeatPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-heat-classifier"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func shipSpeedZeroGlyphPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "ship-speed-zero-glyph"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func flightStatusPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "flight-status"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func requestDockingRangePackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "request-docking-range-classifier"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func leftPanelTabPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "left-panel-tab-state"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func stationServiceFocusPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "station-service-focus"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func eliteActionPackageRoot(t *testing.T, name string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", name))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func flightPromptRawInput(text string, confidence float64) map[string]any {
	return map[string]any{
		"schemaVersion": int64(1),
		"text":          text,
		"confidence":    confidence,
		"evidence":      map[string]any{},
		"model":         map[string]any{},
		"timing":        map[string]any{},
	}
}

func runFlightStatusPackage(t *testing.T, inputs map[string]any) map[string]any {
	t.Helper()
	pkg, err := scriptpackage.Load(flightStatusPackageRoot(t), "elite-dangerous/flight-status")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	output, err := runner.Run(context.Background(), pkg, inputs)
	if err != nil {
		t.Fatalf("run package: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEliteFlightStatusPackageClassifiesFivePassCalibrationCorpus(t *testing.T) {
	tests := []struct {
		sequence   int
		text       string
		confidence float64
		want       string
	}{
		{1, "", 0, "UNKNOWN"},
		{2, "", 0, "UNKNOWN"},
		{3, "AUTO LAUNCH IN PROGRESS", 0.977601, "AUTO_LAUNCH"},
		{4, "WAITINGNUEUE", 0.876809, "WAITING_IN_QUEUE"},
		{5, "WAITING IN QUEUE", 0.891794, "WAITING_IN_QUEUE"},
		{6, "", 0, "UNKNOWN"},
		{7, "", 0, "UNKNOWN"},
		{8, "", 0, "UNKNOWN"},
		{9, "AUTO LAUNCHIN PROGRESS", 0.995785, "AUTO_LAUNCH"},
		{10, "AUTO LAUNCHIN PROGRESS", 0.981162, "AUTO_LAUNCH"},
		{11, "vOAVVM", 0.435403, "UNKNOWN"},
		{12, "RESHAGINGORT", 0.75576, "UNKNOWN"},
		{13, "PRESSTO ABORT", 0.92825, "FSD_CHARGING"},
		{14, "ALIGN WITH TARGET DESTINATION", 0.990222, "FSD_ALIGNMENT_REQUIRED"},
		{15, "ALIGN WITH TARGET DESTINATION", 0.987779, "FSD_ALIGNMENT_REQUIRED"},
		{15, "ALIGN WITH ESCAPE VECTOR", 0.987779, "FSD_ESCAPE_VECTOR_REQUIRED"},
		{16, "の", 0.147477, "UNKNOWN"},
		{17, "", 0, "UNKNOWN"},
		{18, "arbour and a series of services to pilots granted", 0.976558, "UNKNOWN"},
		{19, "とそど", 0.132321, "UNKNOWN"},
		{20, "", 0, "UNKNOWN"},
		{21, "", 0, "UNKNOWN"},
		{22, "", 0, "UNKNOWN"},
		{23, "", 0, "UNKNOWN"},
		{24, "ATO DOCKINPAOGRESS", 0.847308, "AUTO_DOCK"},
		{25, " AUTO DOCKIN PROGRESS", 0.91665, "AUTO_DOCK"},
		{26, "AUTO DOCK IN PROGRESS", 0.95008, "AUTO_DOCK"},
		{27, "", 0, "UNKNOWN"},
		{28, "AUTO DOCKINPROGRESS", 0.849176, "AUTO_DOCK"},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("sequence-%02d", test.sequence), func(t *testing.T) {
			for pass := 1; pass <= 5; pass++ {
				result := runFlightStatusPackage(t, flightPromptRawInput(test.text, test.confidence))
				status := result["flightStatus"].(map[string]any)
				decision := result["decision"].(map[string]any)
				if status["state"] != test.want || status["known"] != (test.want != "UNKNOWN") || decision["accepted"] != (test.want != "UNKNOWN") {
					t.Fatalf("pass %d result = %#v, want %s", pass, result, test.want)
				}
			}
		})
	}
}

func TestEliteFlightStatusPackageRecognizesSupercruise(t *testing.T) {
	result := runFlightStatusPackage(t, flightPromptRawInput("SUPERCRUISE", 0.95))
	status := result["flightStatus"].(map[string]any)
	if status["state"] != "SUPERCRUISE" || status["known"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func TestEliteFlightStatusPackageSeparatesSupercruiseAssistActive(t *testing.T) {
	for _, text := range []string{"SUPERCRUISE ASSIST ACTIVE", "SUPERCRUISEASSISTACTIVE", "SUPERCRUISE ASSIST ACT1VE"} {
		result := runFlightStatusPackage(t, flightPromptRawInput(text, 0.95))
		status := result["flightStatus"].(map[string]any)
		decision := result["decision"].(map[string]any)
		if status["state"] != "SUPERCRUISE_ASSIST_ACTIVE" || status["known"] != true || decision["accepted"] != true {
			t.Fatalf("text %q result = %#v", text, result)
		}
	}
}

func TestEliteFlightStatusPackageRecognizesSafeDisengageReady(t *testing.T) {
	for _, text := range []string{"SAFE DISENGAGE READY", "SAFE DISENGAGEREADY", "SAFE DISENGAGE REAOY"} {
		result := runFlightStatusPackage(t, flightPromptRawInput(text, 0.95))
		status := result["flightStatus"].(map[string]any)
		decision := result["decision"].(map[string]any)
		if status["state"] != "SAFE_DISENGAGE_READY" || status["known"] != true || decision["accepted"] != true {
			t.Fatalf("text %q result = %#v", text, result)
		}
	}
}

func TestEliteFlightStatusPackageRecognizesSlowDownForAutoDock(t *testing.T) {
	for _, text := range []string{
		"SLOW DOWN FOR AUTO DOCK",
		"SLOWDOWNFORAUTODOCK",
		"SLOW DOWN FOR AUTO OOCK",
	} {
		result := runFlightStatusPackage(t, flightPromptRawInput(text, 0.95))
		status := result["flightStatus"].(map[string]any)
		decision := result["decision"].(map[string]any)
		if status["state"] != "SLOW_DOWN_FOR_AUTO_DOCK" || status["known"] != true || decision["accepted"] != true {
			t.Fatalf("text %q result = %#v", text, result)
		}
	}
}

func TestEliteFlightStatusPackageKeepsLowConfidenceAndAmbiguousTextUnknown(t *testing.T) {
	for _, test := range []struct {
		name       string
		text       string
		confidence float64
	}{
		{name: "below confidence threshold", text: "AUTO DOCK IN PROGRESS", confidence: 0.299},
		{name: "slow down below confidence threshold", text: "SLOW DOWN FOR AUTO DOCK", confidence: 0.299},
		{name: "ambiguous launch or dock", text: "AUTO IN PROGRESS", confidence: 0.99},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runFlightStatusPackage(t, flightPromptRawInput(test.text, test.confidence))
			status := result["flightStatus"].(map[string]any)
			decision := result["decision"].(map[string]any)
			if status["state"] != "UNKNOWN" || decision["accepted"] != false {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestEliteFlightStatusPackageRejectsHighConfidenceUnrelatedText(t *testing.T) {
	result := runFlightStatusPackage(t, flightPromptRawInput("CURIULIS STARP", 0.857365))
	status := result["flightStatus"].(map[string]any)
	decision := result["decision"].(map[string]any)
	if status["state"] != "UNKNOWN" || decision["accepted"] != false || decision["candidateState"] != "FSD_CHARGING" {
		t.Fatalf("result = %#v", result)
	}
	if decision["confidence"].(float64) <= decision["threshold"].(float64) ||
		decision["textSimilarity"].(float64) >= decision["similarityThreshold"].(float64) {
		t.Fatalf("similarity gate was not the rejecting evidence: %#v", decision)
	}
}

func TestEliteFlightStatusPackageRejectsMalformedRawOCRInput(t *testing.T) {
	pkg, err := scriptpackage.Load(flightStatusPackageRoot(t), "elite-dangerous/flight-status")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), pkg, map[string]any{"text": "SUPERCRUISE", "confidence": 1.0})
	var runError *Error
	if !errors.As(err, &runError) || runError.Code != "SCRIPT_INPUT_INVALID" {
		t.Fatalf("error = %#v", err)
	}
}

func drawShipStatusBox(pixels []any, left, top int, highlighted bool) {
	color := uint32(0xFF7700)
	if highlighted {
		color = uint32(0x40DDEB)
	}
	for y := top; y < top+15; y++ {
		for x := left; x < left+17; x++ {
			if highlighted || y < top+3 || y >= top+12 || x < left+3 || x >= left+14 {
				pixels[y*270+x] = color
			}
		}
	}
}

func shipStatusPixels(panelVisible bool, highlightedRow int) []any {
	pixels := make([]any, 270*180)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	if panelVisible {
		for row := 0; row < 3; row++ {
			drawShipStatusBox(pixels, 40, 60+row*19, highlightedRow == row)
		}
	}
	return pixels
}

func runShipStatusPackage(t *testing.T, pixels []any) (map[string]any, *shipStatusBroker) {
	t.Helper()
	pkg, err := scriptpackage.Load(shipStatusPackageRoot(t), "elite-dangerous/ship-status")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	broker := &shipStatusBroker{pixels: pixels}
	runner, err := New(broker)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatalf("run package: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	return result, broker
}

func obsoleteEliteShipStatusPackageUsesFixedScreenRegion(t *testing.T) {
	result, broker := runShipStatusPackage(t, shipStatusPixels(true, 0))
	if len(broker.calls) != 1 || broker.calls[0].namespace != "screen" || broker.calls[0].operation != "readRegion" {
		t.Fatalf("calls = %#v", broker.calls)
	}
	wantArguments := map[string]any{
		"x": int64(1650), "y": int64(900), "w": int64(270), "h": int64(180),
		"sampling": "reference",
	}
	if got := broker.calls[0].arguments; !reflect.DeepEqual(got, wantArguments) {
		t.Fatalf("screen arguments = %#v, want %#v", got, wantArguments)
	}
	shipStatus := result["shipStatus"].(map[string]any)
	massLock := shipStatus["massLock"].(map[string]any)
	landingGear := shipStatus["landingGear"].(map[string]any)
	cargoScoop := shipStatus["cargoScoop"].(map[string]any)
	evidence := result["evidence"].(map[string]any)
	if massLock["state"] != "ON" || massLock["on"] != true || massLock["color"] != "cyan" ||
		landingGear["state"] != "OFF" || cargoScoop["state"] != "OFF" ||
		evidence["panelVisible"] != true || evidence["statusTripletDetected"] != true {
		t.Fatalf("result = %#v", result)
	}
}

func obsoleteEliteShipStatusPackageReportsAllRowsOff(t *testing.T) {
	result, _ := runShipStatusPackage(t, shipStatusPixels(true, -1))
	shipStatus := result["shipStatus"].(map[string]any)
	evidence := result["evidence"].(map[string]any)
	for _, name := range []string{"massLock", "landingGear", "cargoScoop"} {
		status := shipStatus[name].(map[string]any)
		if status["state"] != "OFF" || status["on"] != false || status["color"] != "orange" {
			t.Fatalf("%s = %#v", name, status)
		}
	}
	if evidence["panelVisible"] != true {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func obsoleteEliteShipStatusPackageReportsEachHighlightedRowIndependently(t *testing.T) {
	for _, test := range []struct {
		name      string
		row       int
		indicator string
	}{
		{name: "mass lock", row: 0, indicator: "massLock"},
		{name: "landing gear", row: 1, indicator: "landingGear"},
		{name: "cargo scoop", row: 2, indicator: "cargoScoop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, _ := runShipStatusPackage(t, shipStatusPixels(true, test.row))
			shipStatus := result["shipStatus"].(map[string]any)
			for _, name := range []string{"massLock", "landingGear", "cargoScoop"} {
				status := shipStatus[name].(map[string]any)
				wantState := "OFF"
				wantOn := false
				wantColor := "orange"
				if name == test.indicator {
					wantState = "ON"
					wantOn = true
					wantColor = "cyan"
				}
				if status["state"] != wantState || status["on"] != wantOn || status["color"] != wantColor {
					t.Fatalf("%s = %#v, want state=%s on=%v color=%s", name, status, wantState, wantOn, wantColor)
				}
			}
		})
	}
}

func obsoleteEliteShipStatusPackageSeparatesAdjacentFilledRows(t *testing.T) {
	pixels := shipStatusPixels(false, -1)
	drawShipStatusBox(pixels, 40, 60, true)
	drawShipStatusBox(pixels, 40, 79, true)
	drawShipStatusBox(pixels, 40, 98, false)
	result, _ := runShipStatusPackage(t, pixels)
	shipStatus := result["shipStatus"].(map[string]any)
	evidence := result["evidence"].(map[string]any)
	for _, name := range []string{"massLock", "landingGear"} {
		status := shipStatus[name].(map[string]any)
		if status["state"] != "ON" || status["on"] != true || status["color"] != "cyan" {
			t.Fatalf("%s = %#v", name, status)
		}
	}
	cargoScoop := shipStatus["cargoScoop"].(map[string]any)
	if cargoScoop["state"] != "OFF" || cargoScoop["on"] != false || cargoScoop["color"] != "orange" {
		t.Fatalf("cargoScoop = %#v", cargoScoop)
	}
	if evidence["panelVisible"] != true || evidence["statusTripletDetected"] != true {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func obsoleteEliteShipStatusPackageReportsUnknownWhenPanelEvidenceIsInsufficient(t *testing.T) {
	result, _ := runShipStatusPackage(t, shipStatusPixels(false, -1))
	shipStatus := result["shipStatus"].(map[string]any)
	evidence := result["evidence"].(map[string]any)
	for _, name := range []string{"massLock", "landingGear", "cargoScoop"} {
		status := shipStatus[name].(map[string]any)
		if status["state"] != "UNKNOWN" || status["on"] != nil || status["color"] != nil {
			t.Fatalf("%s = %#v", name, status)
		}
	}
	if evidence["panelVisible"] != false {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func obsoleteEliteShipStatusPackageReportsUnknownForIncompleteStatusGroup(t *testing.T) {
	pixels := shipStatusPixels(true, -1)
	for y := 98; y < 113; y++ {
		for x := 40; x < 57; x++ {
			pixels[y*270+x] = uint32(0)
		}
	}
	result, _ := runShipStatusPackage(t, pixels)
	shipStatus := result["shipStatus"].(map[string]any)
	evidence := result["evidence"].(map[string]any)
	for _, name := range []string{"massLock", "landingGear", "cargoScoop"} {
		status := shipStatus[name].(map[string]any)
		if status["state"] != "UNKNOWN" || status["on"] != nil {
			t.Fatalf("%s = %#v", name, status)
		}
	}
	if evidence["statusTripletDetected"] != false {
		t.Fatalf("evidence = %#v", evidence)
	}
}

func obsoleteEliteShipStatusPackageFailsOnMalformedObserverEvidence(t *testing.T) {
	pkg, err := scriptpackage.Load(shipStatusPackageRoot(t), "elite-dangerous/ship-status")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&shipStatusBroker{pixels: []any{uint32(0)}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	var runError *Error
	if !errors.As(err, &runError) || runError.Code != "SHIP_STATUS_EVIDENCE_INVALID" {
		t.Fatalf("error = %#v", err)
	}
}

func shipStatusClassifierInput(texts []string, colors []uint32) map[string]any {
	regions := make([]any, 0, len(texts))
	for index, text := range texts {
		pixels := make([]any, 16*12)
		for pixel := range pixels {
			pixels[pixel] = colors[index]
		}
		regions = append(regions, map[string]any{
			"points": []any{}, "referencePoints": []any{},
			"detectionConfidence": 0.9, "text": text, "recognitionConfidence": 0.9,
			"leftContext": map[string]any{
				"x": int64(0), "y": int64(0), "w": int64(16), "h": int64(12), "pixels": pixels,
				"referenceRegion": map[string]any{"x": 1600.0, "y": 900.0, "w": 16.0, "h": 12.0},
			},
		})
	}
	return map[string]any{
		"schemaVersion": int64(1), "regions": regions,
		"evidence": map[string]any{
			"capturedAt":      "2026-08-08T00:00:00Z",
			"frame":           map[string]any{"width": int64(3840), "height": int64(2160)},
			"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080)},
			"referenceRegion": map[string]any{"x": int64(1600), "y": int64(880), "w": int64(320), "h": int64(150)},
			"physicalRegion":  map[string]any{"left": int64(3200), "top": int64(1760), "width": int64(640), "height": int64(300)},
		},
		"models": map[string]any{}, "timing": map[string]any{},
	}
}

func runShipStatusClassifier(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	pkg, err := scriptpackage.Load(shipStatusPackageRoot(t), "elite-dangerous/ship-status-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runner.Run(context.Background(), pkg, input)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEliteShipStatusClassifierUsesPrefixLabelsAndIndependentColors(t *testing.T) {
	result := runShipStatusClassifier(t, shipStatusClassifierInput(
		[]string{"MASS L0CKED", "LANDING GEAR", "CARGO SCOOP"},
		[]uint32{0x40DDEB, 0xFF7700, 0x40DDEB},
	))
	statuses := result["shipStatus"].(map[string]any)
	if statuses["massLock"].(map[string]any)["state"] != "ON" ||
		statuses["landingGear"].(map[string]any)["state"] != "OFF" ||
		statuses["cargoScoop"].(map[string]any)["state"] != "ON" {
		t.Fatalf("ship status = %#v", statuses)
	}
}

func TestEliteShipStatusClassifierDoesNotGuessMissingLabel(t *testing.T) {
	result := runShipStatusClassifier(t, shipStatusClassifierInput(
		[]string{"LANDING GEAR", "CARGO SCOOP"}, []uint32{0x40DDEB, 0xFF7700},
	))
	mass := result["shipStatus"].(map[string]any)["massLock"].(map[string]any)
	if mass["state"] != "UNKNOWN" || mass["on"] != nil || mass["evidence"].(map[string]any)["reason"] != "LABEL_NOT_CONFIRMED" {
		t.Fatalf("mass lock = %#v", mass)
	}
}

func shipSpeedClassifierInputWithGlyph(rawText string, rawConfidence float64, constrainedText string, constrainedConfidence, margin float64, glyphState string) map[string]any {
	ocr := map[string]any{
		"schemaVersion": int64(1), "text": constrainedText, "confidence": constrainedConfidence,
		"decoding": map[string]any{
			"characterConstraint": "digits", "rawText": rawText, "rawConfidence": rawConfidence,
			"constrainedText": constrainedText, "constrainedConfidence": constrainedConfidence,
			"rawConstraintMargin": margin,
		},
		"evidence": map[string]any{
			"capturedAt":      "2026-08-08T00:00:00Z",
			"frame":           map[string]any{"width": int64(3840), "height": int64(2160)},
			"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
			"referenceRegion": map[string]any{"x": int64(1100), "y": int64(815), "w": int64(65), "h": int64(50)},
			"physicalRegion":  map[string]any{"left": int64(2200), "top": int64(1630), "width": int64(130), "height": int64(100)},
		},
		"model": map[string]any{}, "timing": map[string]any{},
	}
	glyph := map[string]any{
		"schemaVersion":   int64(1),
		"profile":         map[string]any{"width": int64(3840), "height": int64(2160), "capturedAt": "2026-08-08T00:00:00Z"},
		"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
		"region":          map[string]any{"x": int64(1100), "y": int64(815), "w": int64(65), "h": int64(50)},
		"physicalRegion":  map[string]any{"left": int64(2200), "top": int64(1630), "width": int64(130), "height": int64(100)},
		"zeroGlyph":       map[string]any{"state": glyphState, "reason": "FIXTURE", "candidateCount": int64(1), "orangePixelCount": int64(100), "component": map[string]any{}, "thresholds": map[string]any{}},
	}
	return map[string]any{"ocr": ocr, "glyph": glyph}
}

func shipSpeedClassifierInput(rawText string, rawConfidence float64, constrainedText string, constrainedConfidence, margin float64) map[string]any {
	glyphState := "NOT_ZERO"
	if constrainedText == "0" {
		glyphState = "ZERO"
	}
	return shipSpeedClassifierInputWithGlyph(rawText, rawConfidence, constrainedText, constrainedConfidence, margin, glyphState)
}

func runShipSpeedClassifier(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	pkg, err := scriptpackage.Load(shipSpeedPackageRoot(t), "elite-dangerous/ship-speed-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runner.Run(context.Background(), pkg, input)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func shipHeatClassifierInput(rawText string, rawConfidence float64, constrainedText string, constrainedConfidence, margin float64) map[string]any {
	return map[string]any{
		"schemaVersion": int64(1), "text": constrainedText, "confidence": constrainedConfidence,
		"decoding": map[string]any{
			"characterConstraint": "digits", "rawText": rawText, "rawConfidence": rawConfidence,
			"constrainedText": constrainedText, "constrainedConfidence": constrainedConfidence,
			"rawConstraintMargin": margin,
		},
		"evidence": map[string]any{
			"capturedAt":      "2026-08-10T12:00:00Z",
			"frame":           map[string]any{"width": int64(3840), "height": int64(2160)},
			"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
			"referenceRegion": map[string]any{"x": int64(790), "y": int64(795), "w": int64(100), "h": int64(60)},
			"physicalRegion":  map[string]any{"left": int64(1580), "top": int64(1590), "width": int64(200), "height": int64(120)},
		},
		"model": map[string]any{}, "timing": map[string]any{},
	}
}

func runShipHeatClassifier(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	pkg, err := scriptpackage.Load(shipHeatPackageRoot(t), "elite-dangerous/ship-heat-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runner.Run(context.Background(), pkg, input)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEliteShipHeatClassifierRequiresConstrainedTwoOrThreeDigitReading(t *testing.T) {
	known := runShipHeatClassifier(t, shipHeatClassifierInput("054", 0.91, "054", 0.94, 0.03))["heat"].(map[string]any)
	if known["state"] != "KNOWN" || known["percent"] != float64(54) {
		t.Fatalf("known heat=%#v", known)
	}
	unknown := runShipHeatClassifier(t, shipHeatClassifierInput("S4", 0.74, "54", 0.58, 0.16))["heat"].(map[string]any)
	if unknown["state"] != "UNKNOWN" || unknown["percent"] != nil {
		t.Fatalf("unknown heat=%#v", unknown)
	}
}

func TestEliteShipHeatClassifierAcceptsHighConfidenceRawPercentFormat(t *testing.T) {
	heat := runShipHeatClassifier(t, shipHeatClassifierInput("53%", 0.995, "538", 0.668, 0.328))["heat"].(map[string]any)
	if heat["state"] != "KNOWN" || heat["percent"] != float64(53) {
		t.Fatalf("raw percent heat=%#v", heat)
	}
	evidence := heat["evidence"].(map[string]any)
	if evidence["reason"] != "RAW_PERCENT_TEXT_CONFIRMED" {
		t.Fatalf("raw percent evidence=%#v", evidence)
	}
	unknown := runShipHeatClassifier(t, shipHeatClassifierInput("53%", 0.79, "538", 0.668, 0.328))["heat"].(map[string]any)
	if unknown["state"] != "UNKNOWN" {
		t.Fatalf("low-confidence raw percent heat=%#v", unknown)
	}
}

func requestDockingDistanceRegion(text string, detection, recognition float64) map[string]any {
	points := []any{
		map[string]any{"x": 10.0, "y": 10.0}, map[string]any{"x": 90.0, "y": 10.0},
		map[string]any{"x": 90.0, "y": 30.0}, map[string]any{"x": 10.0, "y": 30.0},
	}
	return map[string]any{
		"points": points, "referencePoints": points,
		"detectionConfidence": detection, "text": text, "recognitionConfidence": recognition,
		"leftContext": map[string]any{
			"x": int64(0), "y": int64(10), "w": int64(0), "h": int64(20), "pixels": []any{},
			"referenceRegion": map[string]any{"x": 10.0, "y": 10.0, "w": 0.0, "h": 20.0},
		},
	}
}

func requestDockingRangeClassifierInput(regions []any) map[string]any {
	return map[string]any{
		"schemaVersion": int64(1), "regions": regions,
		"evidence": map[string]any{
			"capturedAt":      "2026-08-08T00:00:00Z",
			"frame":           map[string]any{"width": int64(3840), "height": int64(2160)},
			"coordinateSpace": map[string]any{"width": int64(1920), "height": int64(1080), "fit": "centered-16:9"},
			"referenceRegion": map[string]any{"x": int64(0), "y": int64(730), "w": int64(768), "h": int64(240)},
			"physicalRegion":  map[string]any{"left": int64(0), "top": int64(1460), "width": int64(1536), "height": int64(480)},
		},
		"models": map[string]any{}, "timing": map[string]any{},
	}
}

func runRequestDockingRangeClassifier(t *testing.T, regions []any) map[string]any {
	t.Helper()
	pkg, err := scriptpackage.Load(requestDockingRangePackageRoot(t), "elite-dangerous/request-docking-range-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runner.Run(context.Background(), pkg, requestDockingRangeClassifierInput(regions))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result["requestDockingRange"].(map[string]any)
}

func TestEliteShipSpeedClassifierReturnsConfirmedDisplayValue(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("136", 0.81, "136", 0.81, 0))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "MOVING" || speed["displayValue"] != float64(136) || speed["rawCandidate"] != float64(136) || speed["unit"] != nil ||
		speed["evidence"].(map[string]any)["reason"] != "MOVING_SPEED_CONFIRMED" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierAcceptsDigitsIncludingEight(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("288", 0.89, "288", 0.89, 0))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "MOVING" || speed["displayValue"] != float64(288) || speed["rawCandidate"] != float64(288) {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierDoesNotGuessMissingDigits(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("", 0, "", 0, 0))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "UNKNOWN" || speed["displayValue"] != nil || speed["rawCandidate"] != nil ||
		speed["evidence"].(map[string]any)["reason"] != "DIGIT_TEXT_INVALID" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierAcceptsNearbyRawLetterAndDigitSeven(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("V", 0.62, "7", 0.58, 0.04))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "LOW_SPEED" || speed["displayValue"] != nil || speed["rawCandidate"] != float64(7) ||
		speed["evidence"].(map[string]any)["rawText"] != "V" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierRecognizesSlashedZeroWhenOCRReadsSeven(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInputWithGlyph("H", 0.60, "7", 0.57, 0.03, "ZERO"))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "STOPPED" || speed["displayValue"] != float64(0) || speed["rawCandidate"] != float64(0) ||
		speed["evidence"].(map[string]any)["reason"] != "SLASHED_ZERO_GLYPH_CONFIRMED" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierRecognizesSlashedZeroDespiteWeakOCR(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInputWithGlyph("0", 0.47, "0", 0.47, 0, "ZERO"))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "STOPPED" || speed["displayValue"] != float64(0) || speed["rawCandidate"] != float64(0) ||
		speed["evidence"].(map[string]any)["reason"] != "SLASHED_ZERO_GLYPH_CONFIRMED" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierKeepsRealSevenLowSpeed(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInputWithGlyph("7", 0.90, "7", 0.90, 0, "NOT_ZERO"))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "LOW_SPEED" || speed["displayValue"] != nil || speed["rawCandidate"] != float64(7) {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierRejectsOCRZeroWithoutZeroTopology(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInputWithGlyph("0", 0.90, "0", 0.90, 0, "NOT_ZERO"))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "UNKNOWN" || speed["displayValue"] != nil || speed["rawCandidate"] != nil ||
		speed["evidence"].(map[string]any)["reason"] != "OCR_ZERO_GLYPH_CONFLICT" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierRejectsStrongRawLetterDisagreement(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("V", 0.91, "7", 0.70, 0.21))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "UNKNOWN" || speed["displayValue"] != nil || speed["rawCandidate"] != nil ||
		speed["evidence"].(map[string]any)["reason"] != "RAW_CONSTRAINT_DISAGREEMENT_HIGH" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierReturnsStoppedForQualifiedZero(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("0", 0.92, "0", 0.92, 0))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "STOPPED" || speed["displayValue"] != float64(0) || speed["rawCandidate"] != float64(0) ||
		speed["evidence"].(map[string]any)["reason"] != "SLASHED_ZERO_GLYPH_CONFIRMED" {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteShipSpeedClassifierDoesNotExposeSingleDigitAsDisplayValue(t *testing.T) {
	for _, digit := range []string{"1", "4", "9"} {
		t.Run(digit, func(t *testing.T) {
			result := runShipSpeedClassifier(t, shipSpeedClassifierInput(digit, 0.88, digit, 0.88, 0))
			speed := result["speed"].(map[string]any)
			if speed["state"] != "LOW_SPEED" || speed["displayValue"] != nil || speed["rawCandidate"] != float64(digit[0]-'0') ||
				speed["evidence"].(map[string]any)["reason"] != "LOW_SPEED_RANGE_CONFIRMED" {
				t.Fatalf("speed = %#v", speed)
			}
		})
	}
}

func TestEliteShipSpeedClassifierTreatsTenAsMovingBoundary(t *testing.T) {
	result := runShipSpeedClassifier(t, shipSpeedClassifierInput("10", 0.90, "10", 0.90, 0))
	speed := result["speed"].(map[string]any)
	if speed["state"] != "MOVING" || speed["displayValue"] != float64(10) || speed["rawCandidate"] != float64(10) {
		t.Fatalf("speed = %#v", speed)
	}
}

func TestEliteRequestDockingRangeClassifierAllowsReviewedDistancesBelowGate(t *testing.T) {
	for _, test := range []struct {
		text       string
		wantMeters float64
	}{
		{"CORIOLIS STARPORT 6.25km", 6250},
		{"CORIOLIS STARPORT 5.18 km", 5180},
		{"CORIOLIS STARPORT 698m", 698},
	} {
		t.Run(test.text, func(t *testing.T) {
			regions := []any{
				requestDockingDistanceRegion("CORIOLIS STARPORT", 0.84, 0.99),
				requestDockingDistanceRegion(test.text, 0.82, 0.91),
			}
			rangeResult := runRequestDockingRangeClassifier(t, regions)
			if rangeResult["state"] != "ALLOWED" || rangeResult["allowed"] != true ||
				rangeResult["distanceMeters"] != test.wantMeters ||
				rangeResult["evidence"].(map[string]any)["reason"] != "DISPLAY_DISTANCE_BELOW_THRESHOLD" {
				t.Fatalf("requestDockingRange = %#v", rangeResult)
			}
		})
	}
}

func TestEliteRequestDockingRangeClassifierDeniesThresholdAndLongerUnits(t *testing.T) {
	for _, text := range []string{
		"CORIOLIS STARPORT 7.50km",
		"JAGDBADGER'S REST 8.72km",
		"LP 470-30 4.21Ly",
	} {
		t.Run(text, func(t *testing.T) {
			rangeResult := runRequestDockingRangeClassifier(t, []any{requestDockingDistanceRegion(text, 0.83, 0.88)})
			if rangeResult["state"] != "DENIED" || rangeResult["allowed"] != false ||
				rangeResult["evidence"].(map[string]any)["reason"] != "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD" {
				t.Fatalf("requestDockingRange = %#v", rangeResult)
			}
		})
	}
}

func TestEliteRequestDockingRangeClassifierDoesNotMergeSeparatedNumbers(t *testing.T) {
	rangeResult := runRequestDockingRangeClassifier(t, []any{requestDockingDistanceRegion("LP 470-30 4.21Ly", 0.81, 0.88)})
	if rangeResult["displayText"] != "4.21LY" || rangeResult["distanceValue"] != float64(4.21) ||
		rangeResult["unit"] != "LY" {
		t.Fatalf("requestDockingRange = %#v", rangeResult)
	}
}

func TestEliteRequestDockingRangeClassifierPreservesUnknownEvidence(t *testing.T) {
	for _, test := range []struct {
		name       string
		regions    []any
		wantReason string
	}{
		{"missing regions", []any{}, "DISTANCE_REGIONS_MISSING"},
		{"missing distance", []any{requestDockingDistanceRegion("CORIOLIS STARPORT", 0.82, 0.99)}, "DISTANCE_TEXT_INVALID"},
		{"low detection confidence", []any{requestDockingDistanceRegion("6.25km", 0.69, 0.99)}, "DISTANCE_CONFIDENCE_LOW"},
		{"low recognition confidence", []any{requestDockingDistanceRegion("6.25km", 0.82, 0.74)}, "DISTANCE_CONFIDENCE_LOW"},
		{"malformed", []any{requestDockingDistanceRegion("S.18km", 0.82, 0.90)}, "DISTANCE_TEXT_INVALID"},
		{"malformed repeated unit", []any{requestDockingDistanceRegion("4.77kkm", 0.82, 0.99)}, "DISTANCE_TEXT_INVALID"},
		{"ambiguous same region", []any{requestDockingDistanceRegion("5.18km 698m", 0.82, 0.90)}, "DISTANCE_TEXT_AMBIGUOUS"},
		{"ambiguous separate regions", []any{requestDockingDistanceRegion("5.18km", 0.82, 0.90), requestDockingDistanceRegion("698m", 0.83, 0.91)}, "DISTANCE_TEXT_AMBIGUOUS"},
		{"unknown unit", []any{requestDockingDistanceRegion("6.25parsec", 0.82, 0.90)}, "DISTANCE_TEXT_INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rangeResult := runRequestDockingRangeClassifier(t, test.regions)
			if rangeResult["state"] != "UNKNOWN" || rangeResult["allowed"] != nil ||
				rangeResult["distanceMeters"] != nil ||
				rangeResult["evidence"].(map[string]any)["reason"] != test.wantReason {
				t.Fatalf("requestDockingRange = %#v", rangeResult)
			}
		})
	}
}

var leftPanelTabSampleCoordinates = map[string][2]int64{
	"SYSTEM":       {328, 295},
	"NAVIGATION":   {475, 302},
	"TRANSACTIONS": {720, 298},
	"CONTACTS":     {929, 296},
}

func leftPanelTabSamplePixels(fills map[string]int) map[string][]any {
	result := make(map[string][]any, len(leftPanelTabSampleCoordinates))
	for name, point := range leftPanelTabSampleCoordinates {
		pixels := make([]any, 16)
		for index := range pixels {
			pixels[index] = uint32(0x202020)
		}
		for index := 0; index < fills[name]; index++ {
			pixels[index] = uint32(0xFFAA00)
		}
		result[fmt.Sprintf("%d,%d", point[0], point[1])] = pixels
	}
	return result
}

func TestEliteLeftPanelTabStateClassifiesAllFourTabsAbsentAndAmbiguous(t *testing.T) {
	for _, test := range []struct {
		name, wantState string
		fills           map[string]int
	}{
		{name: "system", wantState: "SYSTEM", fills: map[string]int{"SYSTEM": 16}},
		{name: "navigation", wantState: "NAVIGATION", fills: map[string]int{"NAVIGATION": 16}},
		{name: "transactions", wantState: "TRANSACTIONS", fills: map[string]int{"TRANSACTIONS": 16}},
		{name: "contacts", wantState: "CONTACTS", fills: map[string]int{"CONTACTS": 16}},
		{name: "absent", wantState: "ABSENT"},
		{name: "absent with calibrated cockpit noise", wantState: "ABSENT", fills: map[string]int{"CONTACTS": 4}},
		{name: "above absent noise bound", wantState: "UNKNOWN", fills: map[string]int{"CONTACTS": 5}},
		{name: "insufficient highlight", wantState: "UNKNOWN", fills: map[string]int{"NAVIGATION": 8}},
		{name: "multiple selected", wantState: "UNKNOWN", fills: map[string]int{"NAVIGATION": 16, "TRANSACTIONS": 16}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg, err := scriptpackage.Load(leftPanelTabPackageRoot(t), "elite-dangerous/left-panel-tab-state")
			if err != nil {
				t.Fatal(err)
			}
			broker := &leftPanelTabBroker{pixels: leftPanelTabSamplePixels(test.fills)}
			runner, _ := New(broker)
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			activeTab := result["activeTab"].(map[string]any)
			if activeTab["state"] != test.wantState {
				t.Fatalf("activeTab = %#v", activeTab)
			}
			if len(broker.calls) != 4 {
				t.Fatalf("screen calls = %d, want 4", len(broker.calls))
			}
			for _, call := range broker.calls {
				if call.arguments["w"] != int64(4) || call.arguments["h"] != int64(4) || call.arguments["sampling"] != "reference" {
					t.Fatalf("screen arguments = %#v", call.arguments)
				}
			}
		})
	}
}

func TestEliteLeftPanelTabStateRejectsIncompletePixelEvidence(t *testing.T) {
	pkg, err := scriptpackage.Load(leftPanelTabPackageRoot(t), "elite-dangerous/left-panel-tab-state")
	if err != nil {
		t.Fatal(err)
	}
	pixels := leftPanelTabSamplePixels(nil)
	pixels["328,295"] = make([]any, 10)
	broker := &leftPanelTabBroker{pixels: pixels}
	runner, _ := New(broker)
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "LEFT_PANEL_TAB_EVIDENCE_INVALID") || !strings.Contains(err.Error(), "pixel count is incomplete") {
		t.Fatalf("error=%v", err)
	}
}

var stationServiceTileRanges = map[string][2]int{
	"REFUEL":       {0, 62},
	"REPAIR":       {67, 130},
	"RESTOCK":      {134, 197},
	"LAYER_SWITCH": {202, 264},
}

func stationServiceFocusPixels(highlights map[string]uint32) []any {
	pixels := make([]any, 264*36)
	for index := range pixels {
		pixels[index] = uint32(0x202020)
	}
	for name, color := range highlights {
		xRange := stationServiceTileRanges[name]
		for y := 0; y < 36; y++ {
			for x := xRange[0]; x < xRange[1]; x++ {
				pixels[y*264+x] = color
			}
		}
	}
	return pixels
}

func TestEliteStationServiceFocusClassifiesAllFourRememberedTiles(t *testing.T) {
	for _, test := range []struct {
		name, want string
		color      uint32
	}{
		{name: "refuel orange fill", want: "REFUEL", color: 0xFFA000},
		{name: "repair orange fill", want: "REPAIR", color: 0xFFA000},
		{name: "restock unavailable grey fill", want: "RESTOCK", color: 0xE0E0E0},
		{name: "layer switch orange fill", want: "LAYER_SWITCH", color: 0xFFA000},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg, err := scriptpackage.Load(stationServiceFocusPackageRoot(t), "elite-dangerous/station-service-focus")
			if err != nil {
				t.Fatal(err)
			}
			broker := &stationServiceFocusBroker{pixels: stationServiceFocusPixels(map[string]uint32{test.want: test.color})}
			runner, _ := New(broker)
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			focus := result["focus"].(map[string]any)
			if focus["state"] != test.want {
				t.Fatalf("focus=%#v", focus)
			}
			if len(broker.calls) != 1 || broker.calls[0].arguments["x"] != int64(814) ||
				broker.calls[0].arguments["y"] != int64(759) || broker.calls[0].arguments["w"] != int64(264) ||
				broker.calls[0].arguments["h"] != int64(36) || broker.calls[0].arguments["sampling"] != "reference" {
				t.Fatalf("screen calls=%#v", broker.calls)
			}
		})
	}
}

func TestEliteStationServiceFocusPreservesUnknownEvidence(t *testing.T) {
	for _, test := range []struct {
		name       string
		highlights map[string]uint32
		wantReason string
	}{
		{name: "service row absent", highlights: nil, wantReason: "HIGHLIGHT_LUMINANCE_TOO_LOW"},
		{name: "two bright tiles", highlights: map[string]uint32{"REFUEL": 0xFFA000, "REPAIR": 0xFFA000}, wantReason: "HIGHLIGHT_LUMINANCE_AMBIGUOUS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg, err := scriptpackage.Load(stationServiceFocusPackageRoot(t), "elite-dangerous/station-service-focus")
			if err != nil {
				t.Fatal(err)
			}
			broker := &stationServiceFocusBroker{pixels: stationServiceFocusPixels(test.highlights)}
			runner, _ := New(broker)
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			focus := result["focus"].(map[string]any)
			if focus["state"] != "UNKNOWN" || focus["index"] != nil || focus["reason"] != test.wantReason {
				t.Fatalf("focus=%#v", focus)
			}
		})
	}
}

func TestEliteStationServiceFocusRejectsIncompletePixelEvidence(t *testing.T) {
	pkg, err := scriptpackage.Load(stationServiceFocusPackageRoot(t), "elite-dangerous/station-service-focus")
	if err != nil {
		t.Fatal(err)
	}
	broker := &stationServiceFocusBroker{pixels: make([]any, 10)}
	runner, _ := New(broker)
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "STATION_SERVICE_FOCUS_EVIDENCE_INVALID") || !strings.Contains(err.Error(), "pixel count is incomplete") {
		t.Fatalf("error=%v", err)
	}
}

func requestDockingReferencePoints(left, top float64) []any {
	return []any{
		map[string]any{"x": left, "y": top},
		map[string]any{"x": left + 140, "y": top},
		map[string]any{"x": left + 140, "y": top + 24},
		map[string]any{"x": left, "y": top + 24},
	}
}

func requestDockingRegionsInputAt(text string, detection, recognition float64, bright, dark int, offsetX, offsetY float64, includeAnchor bool) map[string]any {
	regions := []any{}
	if includeAnchor {
		regions = append(regions, map[string]any{
			"points": []any{}, "referencePoints": requestDockingReferencePoints(900+offsetX, 500+offsetY),
			"detectionConfidence": .94, "text": "FACTION", "recognitionConfidence": .99,
			"leftContext": map[string]any{
				"x": int64(0), "y": int64(0), "w": int64(1), "h": int64(1), "pixels": []any{uint32(0x202020)},
				"referenceRegion": map[string]any{"x": 899.0 + offsetX, "y": 500.0 + offsetY, "w": 1.0, "h": 24.0},
			},
		})
	}
	if text != "" {
		pixels := make([]any, 100*50)
		for index := range pixels {
			pixels[index] = uint32(0x202020)
		}
		for index := 0; index < bright; index++ {
			pixels[index] = uint32(0xFFAA00)
		}
		for index := bright; index < bright+dark; index++ {
			pixels[index] = uint32(0x501E10)
		}
		regions = append(regions, map[string]any{
			"points": []any{}, "referencePoints": requestDockingReferencePoints(905+offsetX, 560+offsetY),
			"detectionConfidence": detection, "text": text, "recognitionConfidence": recognition,
			"leftContext": map[string]any{
				"x": int64(0), "y": int64(0), "w": int64(100), "h": int64(50), "pixels": pixels,
				"referenceRegion": map[string]any{"x": 805.0 + offsetX, "y": 552.0 + offsetY, "w": 100.0, "h": 50.0},
			},
		})
	}
	return map[string]any{
		"schemaVersion": int64(1), "regions": regions,
		"evidence": map[string]any{
			"referenceRegion": map[string]any{"x": 700.0, "y": 300.0, "w": 650.0, "h": 400.0},
		},
		"models": map[string]any{}, "timing": map[string]any{"ocrTotalMs": 100.0},
	}
}

func requestDockingRegionsInput(text string, detection, recognition float64, bright, dark int) map[string]any {
	return requestDockingRegionsInputAt(text, detection, recognition, bright, dark, 0, 0, true)
}

func appendRequestDockingTextRegion(input map[string]any, text string, detection, recognition, left, top float64) {
	pixels := make([]any, 100*50)
	for index := range pixels {
		pixels[index] = uint32(0x202020)
	}
	regions := input["regions"].([]any)
	input["regions"] = append(regions, map[string]any{
		"points": []any{}, "referencePoints": requestDockingReferencePoints(left, top),
		"detectionConfidence": detection, "text": text, "recognitionConfidence": recognition,
		"leftContext": map[string]any{
			"x": int64(0), "y": int64(0), "w": int64(100), "h": int64(50), "pixels": pixels,
			"referenceRegion": map[string]any{"x": left - 100, "y": top - 8, "w": 100.0, "h": 50.0},
		},
	})
}

func TestEliteRequestDockingAvailabilityUsesDynamicOCRBoxAndSameFrameFocusPixels(t *testing.T) {
	for _, test := range []struct {
		name, text, want       string
		detection, recognition float64
		bright, dark           int
	}{
		{name: "available", text: "REQUEST DOCKING", detection: .86, recognition: .99, dark: 625, want: "AVAILABLE"},
		{name: "focused", text: "REQUEST DOCKIN", detection: .86, recognition: .91, bright: 750, want: "FOCUSED"},
		{name: "reviewed 4k hdr focused ratio", text: "REQUEST DOCKING", detection: .86, recognition: .99, bright: 450, want: "FOCUSED"},
		{name: "weak bright evidence remains unknown", text: "REQUEST DOCKING", detection: .86, recognition: .99, bright: 350, want: "UNKNOWN"},
		{name: "anchored action absent", want: "UNAVAILABLE"},
		{name: "already active", text: "CANCEL DOCKING", detection: .84, recognition: .95, dark: 625, want: "DOCKING_ACTIVE"},
		{name: "unrelated text proves absent", text: "INTERNAL SECURITY", detection: .90, recognition: .94, want: "UNAVAILABLE"},
		{name: "weak plausible text remains unknown", text: "REQUEST DOCK", detection: .85, recognition: .20, dark: 625, want: "UNKNOWN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
			if err != nil {
				t.Fatal(err)
			}
			inputs := map[string]any{
				"contacts": map[string]any{
					"activeTab": map[string]any{"state": "CONTACTS"},
				},
				"regions": requestDockingRegionsInput(test.text, test.detection, test.recognition, test.bright, test.dark),
			}
			runner, _ := New(&fixtureBroker{})
			output, err := runner.Run(context.Background(), pkg, inputs)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			requestDocking := result["requestDocking"].(map[string]any)
			if requestDocking["state"] != test.want {
				t.Fatalf("requestDocking=%#v output=%s", requestDocking, output)
			}
		})
	}
}

func TestEliteRequestDockingAvailabilityAlignsActionZoneToShiftedFactionAnchor(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&fixtureBroker{})
	for _, offset := range [][2]float64{{0, 0}, {-140, -95}, {170, 110}} {
		inputs := map[string]any{
			"contacts": map[string]any{"activeTab": map[string]any{"state": "CONTACTS"}},
			"regions":  requestDockingRegionsInputAt("REQUEST DOCKING", .90, .98, 0, 625, offset[0], offset[1], true),
		}
		output, err := runner.Run(context.Background(), pkg, inputs)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), `"state":"AVAILABLE"`) || !strings.Contains(string(output), `"reason":"REQUEST_DOCKING_VISIBLE"`) {
			t.Fatalf("offset=%v output=%s", offset, output)
		}
	}
}

func TestEliteRequestDockingAvailabilityRequiresCurrentFrameFactionAnchor(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&fixtureBroker{})
	inputs := map[string]any{
		"contacts": map[string]any{"activeTab": map[string]any{"state": "CONTACTS"}},
		"regions":  requestDockingRegionsInputAt("REQUEST DOCKING", .90, .98, 0, 625, 0, 0, false),
	}
	output, err := runner.Run(context.Background(), pkg, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"state":"UNKNOWN"`) || !strings.Contains(string(output), `"reason":"CONTACTS_ACTION_ANCHOR_NOT_CONFIRMED"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteRequestDockingAvailabilityRejectsPlausibleTextOutsideAnchoredZone(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	input := requestDockingRegionsInputAt("REQUEST DOCKING", .90, .98, 0, 625, 0, 0, true)
	regions := input["regions"].([]any)
	regions[1].(map[string]any)["referencePoints"] = requestDockingReferencePoints(300, 860)
	runner, _ := New(&fixtureBroker{})
	output, err := runner.Run(context.Background(), pkg, map[string]any{
		"contacts": map[string]any{"activeTab": map[string]any{"state": "CONTACTS"}},
		"regions":  input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"state":"UNKNOWN"`) || !strings.Contains(string(output), `"reason":"ACTION_TEXT_OUTSIDE_ANCHORED_ZONE"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteRequestDockingAvailabilityStopsBeforeButtonEvidenceWhenContactsIsNotSelected(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]any{
		"contacts": map[string]any{"activeTab": map[string]any{"state": "ABSENT"}},
		"regions":  nil,
	}
	runner, _ := New(&fixtureBroker{})
	output, err := runner.Run(context.Background(), pkg, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `"state":"UNKNOWN"`) || !strings.Contains(string(output), `"reason":"CONTACTS_TAB_NOT_SELECTED"`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteRequestDockingAvailabilityAcceptsCancelDuringTransientTabDrift(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&fixtureBroker{})
	for _, test := range []struct {
		text, wantState, wantReason string
	}{
		{text: "CANCEL DOCKING", wantState: "DOCKING_ACTIVE", wantReason: "CANCEL_DOCKING_CONFIRMED"},
		{text: "REQUEST DOCKING", wantState: "UNKNOWN", wantReason: "CONTACTS_TAB_NOT_CONFIRMED"},
	} {
		inputs := map[string]any{
			"contacts": map[string]any{"activeTab": map[string]any{"state": "UNKNOWN"}},
			"regions":  requestDockingRegionsInput(test.text, .90, .98, 0, 625),
		}
		output, err := runner.Run(context.Background(), pkg, inputs)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), `"state":"`+test.wantState+`"`) || !strings.Contains(string(output), `"reason":"`+test.wantReason+`"`) {
			t.Fatalf("text=%q output=%s", test.text, output)
		}
	}
}

func TestEliteRequestDockingAvailabilityReportsExplicitDenialNotification(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&fixtureBroker{})
	for _, test := range []struct {
		name, contactsState string
		includeAnchor       bool
	}{
		{name: "denial outranks still-visible request row", contactsState: "CONTACTS", includeAnchor: true},
		{name: "transient tab and missing anchor", contactsState: "UNKNOWN", includeAnchor: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := requestDockingRegionsInputAt("REQUEST DOCKING", .847374, .999730, 0, 625, 0, 0, test.includeAnchor)
			appendRequestDockingTextRegion(input, "DOCKING REQUEST DENIED.", .845842, .999919, 995, 623)
			output, err := runner.Run(context.Background(), pkg, map[string]any{
				"contacts": map[string]any{"activeTab": map[string]any{"state": test.contactsState}},
				"regions":  input,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{
				`"state":"DENIED"`,
				`"available":null`,
				`"focused":null`,
				`"text":"DOCKING REQUEST DENIED."`,
				`"reason":"DOCKING_REQUEST_DENIED_CONFIRMED"`,
			} {
				if !strings.Contains(string(output), expected) {
					t.Fatalf("missing %s in output=%s", expected, output)
				}
			}
			if !test.includeAnchor && !strings.Contains(string(output), `"anchor":null`) {
				t.Fatalf("unaccepted anchor must remain null: output=%s", output)
			}
		})
	}
}

func TestEliteRequestDockingAvailabilityPrefersConfirmedCancelOverSameFrameDenial(t *testing.T) {
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "request-docking-availability-classifier"), "elite-dangerous/request-docking-availability-classifier")
	if err != nil {
		t.Fatal(err)
	}
	input := requestDockingRegionsInputAt("CANCEL DOCKING", .91, .99, 0, 625, 0, 0, true)
	appendRequestDockingTextRegion(input, "DOCKING REQUEST DENIED.", .845842, .999919, 995, 623)
	runner, _ := New(&fixtureBroker{})
	output, err := runner.Run(context.Background(), pkg, map[string]any{
		"contacts": map[string]any{"activeTab": map[string]any{"state": "CONTACTS"}},
		"regions":  input,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"state":"DOCKING_ACTIVE"`,
		`"denialNotificationDetected":true`,
		`"denialNotificationOverridden":true`,
		`"reason":"CANCEL_DOCKING_OVERRIDES_DENIAL_NOTIFICATION"`,
	} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("missing %s in output=%s", expected, output)
		}
	}
}

func TestEliteCompassPackageUsesFixedScreenRegion(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for y := 40; y < 43; y++ {
		for x := 58; x < 62; x++ {
			pixels[y*96+x] = uint32(0x40DDEB)
		}
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	broker := &compassBroker{pixels: pixels}
	runner, err := New(broker)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatalf("run package: %v", err)
	}
	if len(broker.calls) != 1 || broker.calls[0].namespace != "screen" || broker.calls[0].operation != "readRegion" {
		t.Fatalf("calls = %#v", broker.calls)
	}
	wantArguments := map[string]any{
		"x": int64(682), "y": int64(771), "w": int64(96), "h": int64(96),
		"sampling": "reference",
	}
	if got := broker.calls[0].arguments; !reflect.DeepEqual(got, wantArguments) {
		t.Fatalf("screen arguments = %#v, want %#v", got, wantArguments)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	zone := target["centerZone"].(map[string]any)
	if target["detected"] != true || target["offsetX"] != float64(11) || target["offsetY"] != float64(-7) ||
		math.Abs(target["screenAngleDegrees"].(float64)-57.529) > 0.0001 ||
		math.Abs(target["centerDistancePixels"].(float64)-13.038) > 0.0001 ||
		zone["shape"] != "circle" || zone["radiusPixels"] != float64(4) || zone["inside"] != false {
		t.Fatalf("target = %#v", target)
	}
}

func TestEliteHyperspaceTargetOcclusionReportsCoverageAndEscapeDirection(t *testing.T) {
	pkg, err := scriptpackage.Load(hyperspaceTargetOcclusionPackageRoot(t), "elite-dangerous/hyperspace-target-occlusion")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		paint       func(map[int64][]any)
		wantState   string
		wantControl any
		wantSafe    bool
	}{
		{
			name: "full stellar disc has no invented direction",
			paint: func(strips map[int64][]any) {
				for _, pixels := range strips {
					for index := range pixels {
						pixels[index] = uint32(0xFFF090)
					}
				}
			},
			wantState: "BLOCKING", wantControl: nil, wantSafe: false,
		},
		{
			name: "lower stellar disc recommends pitch up",
			paint: func(strips map[int64][]any) {
				pixels := strips[580]
				for index := range pixels {
					pixels[index] = uint32(0xFFB020)
				}
			},
			wantState: "BLOCKING", wantControl: "PITCH_UP", wantSafe: false,
		},
		{
			name: "top canopy stellar disc clamps public centroid and recommends pitch down",
			paint: func(strips map[int64][]any) {
				pixels := strips[20]
				for index := range pixels {
					pixels[index] = uint32(0xFFB020)
				}
			},
			wantState: "BLOCKING", wantControl: "PITCH_DOWN", wantSafe: false,
		},
		{
			name: "empty starfield is clear",
			paint: func(map[int64][]any) {
			},
			wantState: "CLEAR", wantControl: nil, wantSafe: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			strips := map[int64][]any{}
			for _, y := range []int64{20, 160, 300, 440, 580} {
				strips[y] = make([]any, 1680*7)
				for index := range strips[y] {
					strips[y][index] = uint32(0)
				}
			}
			test.paint(strips)
			broker := &wideOcclusionBroker{strips: strips}
			runner, err := New(broker)
			if err != nil {
				t.Fatal(err)
			}
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			occlusion := result["occlusion"].(map[string]any)
			if occlusion["state"] != test.wantState || occlusion["recommendedControl"] != test.wantControl || occlusion["safeToCharge"] != test.wantSafe {
				t.Fatalf("occlusion=%#v", occlusion)
			}
			if occlusion["sampledPixelCount"] != float64(58800) || len(occlusion["gridCoverageRatios"].([]any)) != 25 {
				t.Fatalf("sampling evidence=%#v", occlusion)
			}
			if occlusion["centroidY"] != nil && (occlusion["centroidY"].(float64) < 0 || occlusion["centroidY"].(float64) > 899) {
				t.Fatalf("centroidY outside public ROI coordinates: %#v", occlusion["centroidY"])
			}
			wantArguments := map[string]any{"x": int64(120), "y": int64(20), "w": int64(1680), "h": int64(7), "sampling": "reference"}
			if len(broker.calls) != 5 || !reflect.DeepEqual(broker.calls[0].arguments, wantArguments) {
				t.Fatalf("calls=%#v", broker.calls)
			}
		})
	}
}

func TestEliteCockpitHUDPresenceSeparatesPresentAndAbsent(t *testing.T) {
	pkg, err := scriptpackage.Load(cockpitHUDPresencePackageRoot(t), "elite-dangerous/cockpit-hud-presence")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		orange    int
		cyan      int
		wantState string
	}{
		{name: "reviewed HUD is present", orange: 250, wantState: "PRESENT"},
		{name: "FSD charge cyan HUD is present", cyan: 250, wantState: "PRESENT"},
		{name: "insufficient HUD evidence is absent", orange: 249, wantState: "ABSENT"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pixels := make([]any, 120*120)
			for index := range pixels {
				pixels[index] = uint32(0)
			}
			for index := 0; index < test.orange; index++ {
				pixels[index] = uint32(0xFF7700)
			}
			for index := test.orange; index < test.orange+test.cyan; index++ {
				pixels[index] = uint32(0x40DDEB)
			}
			runner, _ := New(&compassBroker{pixels: pixels})
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), `"state":"`+test.wantState+`"`) || !strings.Contains(string(output), `"orangePixelCount":`+fmt.Sprint(test.orange)) || !strings.Contains(string(output), `"chargeCyanPixelCount":`+fmt.Sprint(test.cyan)) {
				t.Fatalf("output=%s", output)
			}
		})
	}
}

func TestEliteCompassPackageReportsZeroDistanceAndUndefinedAngleAtCenter(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for y := 46; y < 51; y++ {
		for x := 46; x < 51; x++ {
			pixels[y*96+x] = uint32(0x40DDEB)
		}
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	zone := target["centerZone"].(map[string]any)
	if target["detected"] != true || target["presentation"] != "SOLID" || target["hemisphere"] != "FRONT" ||
		target["coreCyanPixelCount"] != float64(25) || target["offsetX"] != float64(0) || target["offsetY"] != float64(0) ||
		target["screenAngleDegrees"] != nil || target["centerDistancePixels"] != float64(0) || zone["inside"] != true {
		t.Fatalf("target = %#v", target)
	}
}

func TestEliteCompassPackageClassifiesHollowRearMarker(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for position := 45; position <= 51; position++ {
		pixels[45*96+position] = uint32(0x40DDEB)
		pixels[51*96+position] = uint32(0x40DDEB)
		pixels[position*96+45] = uint32(0x40DDEB)
		pixels[position*96+51] = uint32(0x40DDEB)
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	if target["detected"] != true || target["presentation"] != "HOLLOW" || target["hemisphere"] != "REAR" ||
		target["coreCyanPixelCount"] != float64(0) || target["centerDistancePixels"] != float64(0) {
		t.Fatalf("target = %#v", target)
	}
}

func TestEliteCompassPackageClassifiesAntialiasedHollowRearMarker(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for position := 45; position <= 51; position++ {
		pixels[45*96+position] = uint32(0x40DDEB)
		pixels[51*96+position] = uint32(0x40DDEB)
		pixels[position*96+45] = uint32(0x40DDEB)
		pixels[position*96+51] = uint32(0x40DDEB)
	}
	// Reference downsampling can retain three faint center pixels from a
	// visually hollow 4K ring. This stays well below the seven-pixel SOLID Gate.
	for _, x := range []int{47, 48, 49} {
		pixels[48*96+x] = uint32(0x40DDEB)
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	if target["presentation"] != "HOLLOW" || target["hemisphere"] != "REAR" || target["centerCyanPixelCount"] != float64(3) {
		t.Fatalf("target=%#v", target)
	}
}

func TestEliteCompassPackagePreprocessesDarkHollowRearMarker(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	// This reviewed topology mirrors the live rear marker. Its darker cyan
	// (110,143,129) is below the retired absolute green threshold.
	ring := map[int][]int{
		-3: {-1, 0, 1},
		-2: {-2, -1, 0, 1, 2},
		-1: {-3, -2, 2, 3},
		0:  {-3, 3},
		1:  {-4, -3, 3, 4},
		2:  {-3, 3},
		3:  {-3, -2, -1, 0, 1, 2, 3},
		4:  {-1, 0, 1, 2},
	}
	for offsetY, offsetsX := range ring {
		for _, offsetX := range offsetsX {
			pixels[(48+offsetY)*96+48+offsetX] = uint32(0x6E8F81)
		}
	}
	// A chromatically valid but isolated pixel must not affect bounds or
	// topology after preprocessing.
	pixels[10*96+10] = uint32(0x6E8F81)

	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result["schemaVersion"] != float64(4) {
		t.Fatalf("schemaVersion=%#v output=%s", result["schemaVersion"], output)
	}
	target := result["target"].(map[string]any)
	bounds := target["markerBounds"].(map[string]any)
	if target["detected"] != true || target["presentation"] != "HOLLOW" || target["hemisphere"] != "REAR" ||
		target["candidateCyanPixelCount"] != float64(32) || target["cyanPixelCount"] != float64(31) ||
		target["centerCyanPixelCount"] != float64(0) ||
		bounds["x"] != float64(726) || bounds["y"] != float64(816) ||
		bounds["w"] != float64(9) || bounds["h"] != float64(8) ||
		bounds["centerX"] != float64(730) || bounds["centerY"] != float64(819) {
		t.Fatalf("target=%#v output=%s", target, output)
	}
}

func TestEliteCompassPackageDiscardsIsolatedCyanNoise(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for _, position := range [][2]int{{10, 10}, {30, 20}, {70, 60}, {80, 80}} {
		pixels[position[1]*96+position[0]] = uint32(0x6E8F81)
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	if target["detected"] != false || target["candidateCyanPixelCount"] != float64(4) ||
		target["cyanPixelCount"] != float64(0) || target["markerBounds"] != nil ||
		target["presentation"] != "UNKNOWN" || target["hemisphere"] != "UNKNOWN" {
		t.Fatalf("target=%#v output=%s", target, output)
	}
}

func TestEliteCompassPackageFailsWhenCompassEvidenceIsAbsent(t *testing.T) {
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	var runError *Error
	if !errors.As(err, &runError) || runError.Code != "COMPASS_NOT_VISIBLE" {
		t.Fatalf("error = %#v", err)
	}
}

func TestEliteCompassPackageReturnsNoTargetWithoutSubstitution(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	pkg, err := scriptpackage.Load(compassPackageRoot(t), "elite-dangerous/compass")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&compassBroker{pixels: pixels})
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	target := result["target"].(map[string]any)
	zone := target["centerZone"].(map[string]any)
	if target["detected"] != false || target["referenceX"] != nil || target["offsetX"] != nil ||
		target["screenAngleDegrees"] != nil || target["centerDistancePixels"] != nil || zone["inside"] != nil {
		t.Fatalf("target = %#v", target)
	}
}

func (b *fixtureBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected fixture blob path request")
}

func (b *fixtureBroker) RecordNative(_ context.Context, record NativeRecord) error {
	b.calls = append(b.calls, "native."+record.Action+"."+record.Phase)
	return nil
}

func (b *fixtureBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, namespace+"."+operation)
	b.observerCalls = append(b.observerCalls, fixtureObserverCall{
		namespace: namespace,
		operation: operation,
		arguments: arguments,
	})
	switch operation {
	case "modules":
		return map[string]any{
			"process": map[string]any{"imageSha256": crimsonImageSHA256},
			"modules": []any{map[string]any{
				"name":        "CrimsonDesert.exe",
				"baseAddress": "0x0000000140000000",
				"size":        int64(367640576),
			}},
		}, nil
	case "scan":
		return map[string]any{"matches": []any{
			map[string]any{"address": "0x000000014071FDEA"},
		}}, nil
	case "resolveRip":
		return map[string]any{"targetAddress": "0x00000001461FE780"}, nil
	case "readBatch":
		reads := arguments["reads"].([]any)
		if len(reads) == 2 {
			want := []any{
				map[string]any{"address": "0x800000", "type": "pointer"},
				map[string]any{"address": "0x80000c", "type": "u16"},
			}
			if !reflect.DeepEqual(reads, want) {
				return nil, errors.New("unexpected fixture inventory header reads")
			}
			return map[string]any{"reads": []any{
				map[string]any{"value": uint64(0x900000)},
				map[string]any{"value": uint64(3)},
			}}, nil
		}
		address := reads[0].(map[string]any)["address"].(string)
		pointers := map[string]uint64{
			"0x00000001461FE780": 0x200000,
			"0x200028":           0x300000,
			"0x3000d0":           0x400000,
			"0x400068":           0x500000,
			"0x5000b8":           0x600000,
			"0x600018":           0x700000,
			"0x700008":           0x800000,
		}
		value, ok := pointers[address]
		if !ok {
			return nil, errors.New("unexpected fixture pointer address " + address)
		}
		return map[string]any{"reads": []any{map[string]any{"value": value}}}, nil
	case "readStrided":
		return map[string]any{"records": []any{
			map[string]any{"index": int64(0), "itemId": uint64(120), "pairedItemId": uint64(120), "quantity": uint64(3)},
			map[string]any{"index": int64(1), "itemId": uint64(0), "pairedItemId": uint64(0), "quantity": uint64(0)},
			map[string]any{"index": int64(2), "itemId": uint64(450), "pairedItemId": uint64(450), "quantity": uint64(1)},
		}}, nil
	default:
		return nil, errors.New("unexpected fixture operation " + operation)
	}
}

func TestCrimsonInventoryPackageFixture(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	broker := &fixtureBroker{}
	runner, err := New(broker)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatalf("run package: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	inventory := result["inventory"].(map[string]any)
	if got := inventory["occupiedCount"]; got != float64(2) {
		t.Fatalf("occupiedCount = %#v, want 2", got)
	}
	wantCalls := []string{
		"memory.modules",
		"memory.scan",
		"memory.resolveRip",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readBatch",
		"memory.readStrided",
	}
	if !reflect.DeepEqual(broker.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", broker.calls, wantCalls)
	}
	if len(broker.observerCalls) != len(wantCalls) {
		t.Fatalf("observer calls = %d, want %d", len(broker.observerCalls), len(wantCalls))
	}
	wantScan := map[string]any{
		"pattern": inventoryManagerPatternForTest,
		"regions": []any{map[string]any{
			"base_address": "0x0000000140000000",
			"size":         int64(367640576),
		}},
		"max_matches": int64(2),
	}
	if got := broker.observerCalls[1].arguments; !reflect.DeepEqual(got, wantScan) {
		t.Fatalf("scan arguments = %#v, want %#v", got, wantScan)
	}
	wantStrided := map[string]any{
		"base_address": "0x900000",
		"count":        int64(3),
		"stride":       int64(0xC8),
		"fields": []any{
			map[string]any{"name": "itemId", "offset": int64(0x08), "type": "u16"},
			map[string]any{"name": "quantity", "offset": int64(0x10), "type": "u64"},
			map[string]any{"name": "pairedItemId", "offset": int64(0x90), "type": "u16"},
		},
	}
	if got := broker.observerCalls[len(broker.observerCalls)-1].arguments; !reflect.DeepEqual(got, wantStrided) {
		t.Fatalf("readStrided arguments = %#v, want %#v", got, wantStrided)
	}
}

func TestInventoryMaximumSchemaOutputFitsRunnerAndProtocolLimits(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	const maxRecords = 2048
	items := make([]any, maxRecords)
	for index := range items {
		items[index] = map[string]any{
			"slot":         uint64(4294967295),
			"itemId":       uint64(4294967295),
			"quantity":     uint64(18446744073709551615),
			"pairedItemId": nil,
			"inventoryKey": uint64(4294967295),
			"instanceId":   uint64(18446744073709551615),
		}
	}
	output := map[string]any{
		"schemaVersion": int64(1),
		"source": map[string]any{
			"kind": "save-file", "processImageSha256": nil,
			"saveModifiedAt": "2026-07-27T13:42:00Z", "nativeLibrary": "save-decoder",
		},
		"attempts": []any{
			map[string]any{"source": "process-memory", "status": "failed", "errorCode": "INVENTORY_SIGNATURE_AMBIGUOUS"},
			map[string]any{"source": "save-file", "status": "succeeded", "errorCode": nil},
		},
		"inventory": map[string]any{
			"recordCount": int64(maxRecords), "occupiedCount": int64(maxRecords), "items": items,
		},
	}
	if err := pkg.ValidateOutput(output); err != nil {
		t.Fatalf("maximum declared output does not satisfy schema: %v", err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(encoded)) > pkg.Manifest.Limits.MaxResultBytes {
		t.Fatalf("maximum schema output is %d bytes, runner limit is %d", len(encoded), pkg.Manifest.Limits.MaxResultBytes)
	}
	rawRecords := make([]any, maxRecords)
	for index := range rawRecords {
		rawRecords[index] = map[string]any{
			"blockIndex":            uint64(4294967295),
			"inventoryElementIndex": uint64(4294967295),
			"itemElementIndex":      uint64(4294967295),
			"inventoryKey":          uint64(4294967295),
			"itemKey":               uint64(4294967295),
			"transferredItemKey":    uint64(4294967295),
			"slotNumber":            uint64(4294967295),
			"flags":                 uint64(4294967295),
			"itemNumber":            uint64(18446744073709551615),
			"stackCount":            uint64(18446744073709551615),
		}
	}
	nativeResult, err := json.Marshal(map[string]any{
		"result": int64(0),
		"out":    []any{rawRecords, int64(maxRecords), uint64(18446744073709551615)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(nativeResult)) > pkg.Manifest.Limits.MaxResultBytes {
		t.Fatalf("maximum decoded native result is %d bytes, runner limit is %d", len(nativeResult), pkg.Manifest.Limits.MaxResultBytes)
	}
	const frameEnvelopeHeadroom = 4096
	if pkg.Manifest.Limits.MaxResultBytes+frameEnvelopeHeadroom >= observationprotocol.DefaultMaxFrameBytes {
		t.Fatalf("runner result limit %d leaves insufficient framed-protocol headroom", pkg.Manifest.Limits.MaxResultBytes)
	}
	library := pkg.Manifest.NativeLibraries["save-decoder"]
	if library.MaxNativeMemoryBytes != 128<<10 {
		t.Fatalf("native memory limit = %d, want 128 KiB", library.MaxNativeMemoryBytes)
	}
}

const inventoryManagerPatternForTest = "?? 89 ?? ?? ?? ?? ?? ?? 8D ?? 30 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 8D ?? B0 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 88"

type ambiguousBroker struct{ fixtureBroker }

func (b *ambiguousBroker) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	if operation == "scan" {
		return map[string]any{"matches": []any{}}, nil
	}
	if namespace == "file" {
		return nil, &BrokerError{
			Code:             "OBSERVER_CALL_FAILED",
			FallbackEligible: true,
			Cause:            errors.New("fixture save decode failure"),
		}
	}
	return b.fixtureBroker.Call(ctx, namespace, operation, arguments)
}

func TestBothSourceFailuresHaveTerminalCode(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&ambiguousBroker{})
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	var runError *Error
	if !errors.As(err, &runError) {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if runError.Code != "INVENTORY_ALL_SOURCES_FAILED" {
		t.Fatalf("code = %q", runError.Code)
	}
}

type saveFallbackBroker struct{ fixtureBroker }

func (b *saveFallbackBroker) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	if operation == "scan" {
		b.calls = append(b.calls, namespace+"."+operation)
		b.observerCalls = append(b.observerCalls, fixtureObserverCall{
			namespace: namespace, operation: operation, arguments: arguments,
		})
		return map[string]any{"matches": []any{}}, nil
	}
	if operation == "list" {
		b.calls = append(b.calls, namespace+"."+operation)
		b.observerCalls = append(b.observerCalls, fixtureObserverCall{
			namespace: namespace, operation: operation, arguments: arguments,
		})
		return map[string]any{"entries": []any{
			map[string]any{
				"relative":   "account",
				"kind":       "directory",
				"modifiedAt": "2026-07-27T13:00:00.000000000Z",
			},
			map[string]any{
				"relative":   "account/slot",
				"kind":       "directory",
				"modifiedAt": "2026-07-27T13:30:00.000000000Z",
			},
			map[string]any{
				"relative":   "account/slot/save.save",
				"kind":       "file",
				"size":       int64(1024),
				"modifiedAt": "2026-07-27T13:42:00.000000000Z",
			},
		}}, nil
	}
	if operation == "openBlob" {
		b.calls = append(b.calls, namespace+"."+operation)
		b.observerCalls = append(b.observerCalls, fixtureObserverCall{
			namespace: namespace, operation: operation, arguments: arguments,
		})
		return map[string]any{
			"modifiedAt": "2026-07-27T13:42:00Z",
			"blob":       map[string]any{"blobHandle": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		}, nil
	}
	return b.fixtureBroker.Call(ctx, namespace, operation, arguments)
}

func (b *saveFallbackBroker) BlobPath(_ context.Context, _ map[string]any) (string, error) {
	b.calls = append(b.calls, "native.blob_path")
	return `C:\job\blob.save`, nil
}

type saveSelectionBroker struct {
	saveFallbackBroker
	entries []any
}

func (b *saveSelectionBroker) Call(
	ctx context.Context,
	namespace, operation string,
	arguments map[string]any,
) (any, error) {
	if operation == "list" {
		b.calls = append(b.calls, namespace+"."+operation)
		b.observerCalls = append(b.observerCalls, fixtureObserverCall{
			namespace: namespace, operation: operation, arguments: arguments,
		})
		return map[string]any{"entries": b.entries}, nil
	}
	return b.saveFallbackBroker.Call(ctx, namespace, operation, arguments)
}

type fixtureNativeBackend struct{}
type fixtureNativeDLL struct{}
type fixtureNativeProcedure struct{ name string }

type failureNativeState struct {
	mode  string
	frees int
}

type failureNativeBackend struct{ state *failureNativeState }
type failureNativeDLL struct{ state *failureNativeState }
type failureNativeProcedure struct {
	name  string
	state *failureNativeState
}

func (fixtureNativeBackend) load(string) (nativeDLL, error) {
	return fixtureNativeDLL{}, nil
}

func (fixtureNativeDLL) bind(name string) (nativeProcedure, error) {
	switch name {
	case "crimson_save_load_from_file", "crimson_save_list_inventory_items", "crimson_save_free":
		return fixtureNativeProcedure{name: name}, nil
	default:
		return nil, errors.New("fixture export is absent")
	}
}

func (fixtureNativeDLL) close() error { return nil }

func (b failureNativeBackend) load(string) (nativeDLL, error) {
	return failureNativeDLL{state: b.state}, nil
}

func (d failureNativeDLL) bind(name string) (nativeProcedure, error) {
	switch name {
	case "crimson_save_load_from_file", "crimson_save_list_inventory_items", "crimson_save_free":
		return failureNativeProcedure{name: name, state: d.state}, nil
	default:
		return nil, errors.New("fixture export is absent")
	}
}

func (failureNativeDLL) close() error { return nil }

func (p failureNativeProcedure) call(frame nativeCallFrame) (uintptr, error) {
	switch p.name {
	case "crimson_save_load_from_file":
		result, err := (fixtureNativeProcedure{name: p.name}).call(frame)
		if p.state.mode == "load" {
			return 1, err
		}
		return result, err
	case "crimson_save_list_inventory_items":
		result, err := (fixtureNativeProcedure{name: p.name}).call(frame)
		if err != nil {
			return result, err
		}
		if frame.arguments[1] == 0 {
			switch p.state.mode {
			case "query":
				return 1, nil
			case "count":
				binary.LittleEndian.PutUint64(frame.buffers[3], 2049)
			}
			return result, nil
		}
		switch p.state.mode {
		case "read":
			return 1, nil
		case "changed":
			binary.LittleEndian.PutUint64(frame.buffers[3], 3)
		}
		return result, nil
	case "crimson_save_free":
		p.state.frees++
		return 0, nil
	default:
		return 0, errors.New("unexpected fixture procedure")
	}
}

func (p fixtureNativeProcedure) call(frame nativeCallFrame) (uintptr, error) {
	switch p.name {
	case "crimson_save_load_from_file":
		binary.LittleEndian.PutUint64(frame.buffers[1], 0x1234)
		return 0, nil
	case "crimson_save_list_inventory_items":
		binary.LittleEndian.PutUint64(frame.buffers[3], 2)
		binary.LittleEndian.PutUint64(frame.buffers[4], 1)
		if frame.arguments[1] == 0 {
			status := int32(-11)
			return uintptr(uint32(status)), nil
		}
		buffer := frame.buffers[1]
		writeRecord := func(offset int, inventoryKey, itemKey, slot uint32, instance, quantity uint64) {
			binary.LittleEndian.PutUint32(buffer[offset+12:], inventoryKey)
			binary.LittleEndian.PutUint32(buffer[offset+16:], itemKey)
			binary.LittleEndian.PutUint32(buffer[offset+24:], slot)
			binary.LittleEndian.PutUint64(buffer[offset+32:], instance)
			binary.LittleEndian.PutUint64(buffer[offset+40:], quantity)
		}
		writeRecord(0, 2, 11, 4, 9001, 77789)
		writeRecord(48, 2, 50001, 5, 9002, 683)
		return 0, nil
	case "crimson_save_free":
		return 0, nil
	default:
		return 0, errors.New("unexpected fixture procedure")
	}
}

func TestMemoryFailureUsesPackageSelectedSave(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	broker := &saveFallbackBroker{}
	runner, _ := New(broker)
	runner.nativeBackend = fixtureNativeBackend{}
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatalf("run package: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	source := result["source"].(map[string]any)
	if source["kind"] != "save-file" {
		t.Fatalf("source = %#v", source)
	}
	inventory := result["inventory"].(map[string]any)
	if inventory["recordCount"] != float64(2) || inventory["occupiedCount"] != float64(2) {
		t.Fatalf("inventory counts = %#v", inventory)
	}
	first := inventory["items"].([]any)[0].(map[string]any)
	if first["slot"] != float64(4) || first["itemId"] != float64(11) ||
		first["quantity"] != float64(77789) || first["inventoryKey"] != float64(2) ||
		first["instanceId"] != float64(9001) {
		t.Fatalf("first decoded record = %#v", first)
	}
	attempts := result["attempts"].([]any)
	if len(attempts) != 2 ||
		attempts[0].(map[string]any)["status"] != "failed" ||
		attempts[1].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("attempts = %#v", attempts)
	}
	if len(broker.calls) < 5 ||
		!reflect.DeepEqual(broker.calls[:5], []string{
			"memory.modules", "memory.scan", "file.list", "file.openBlob", "native.blob_path",
		}) {
		t.Fatalf("call prefix = %#v", broker.calls)
	}
	wantOpenBlob := map[string]any{
		"path": map[string]any{
			"root":     "crimson-desert-saves",
			"relative": "account/slot/save.save",
		},
	}
	if got := broker.observerCalls[len(broker.observerCalls)-1].arguments; !reflect.DeepEqual(got, wantOpenBlob) {
		t.Fatalf("openBlob arguments = %#v, want %#v", got, wantOpenBlob)
	}
	wantList := map[string]any{
		"path":       map[string]any{"root": "crimson-desert-saves", "relative": "."},
		"maxDepth":   int64(3),
		"maxEntries": int64(4096),
	}
	if got := broker.observerCalls[2].arguments; !reflect.DeepEqual(got, wantList) {
		t.Fatalf("list arguments = %#v, want %#v", got, wantList)
	}
	nativeCompleted := 0
	for _, call := range broker.calls {
		if call == "native.call.completed" {
			nativeCompleted++
		}
	}
	if nativeCompleted != 4 {
		t.Fatalf("completed native calls = %d, want 4; calls=%#v", nativeCompleted, broker.calls)
	}
}

func TestPackageOwnedSaveSelectionRejectsAmbiguity(t *testing.T) {
	tests := []struct {
		name    string
		entries []any
		code    string
	}{
		{
			name: "no account",
			code: "ACCOUNT_SAVE_ROOT_NOT_FOUND",
		},
		{
			name: "multiple accounts",
			entries: []any{
				map[string]any{"relative": "one", "kind": "directory", "modifiedAt": "2026-07-27T13:00:00.000000000Z"},
				map[string]any{"relative": "two", "kind": "directory", "modifiedAt": "2026-07-27T13:00:00.000000000Z"},
			},
			code: "ACCOUNT_SAVE_ROOT_AMBIGUOUS",
		},
		{
			name: "missing candidate",
			entries: []any{
				map[string]any{"relative": "account", "kind": "directory", "modifiedAt": "2026-07-27T13:00:00.000000000Z"},
			},
			code: "SAVE_CANDIDATE_NOT_FOUND",
		},
		{
			name: "newest timestamp tie",
			entries: []any{
				map[string]any{"relative": "account", "kind": "directory", "modifiedAt": "2026-07-27T13:00:00.000000000Z"},
				map[string]any{"relative": "account/one/save.save", "kind": "file", "size": int64(1), "modifiedAt": "2026-07-27T13:42:00.000000000Z"},
				map[string]any{"relative": "account/two/save.save", "kind": "file", "size": int64(1), "modifiedAt": "2026-07-27T13:42:00.000000000Z"},
			},
			code: "NEWEST_SAVE_TIMESTAMP_TIE",
		},
		{
			name: "reparse point",
			entries: []any{
				map[string]any{"relative": "account", "kind": "directory", "modifiedAt": "2026-07-27T13:00:00.000000000Z"},
				map[string]any{"relative": "account/link", "kind": "reparse-point"},
			},
			code: "SAVE_REPARSE_POINT_FOUND",
		},
	}
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker := &saveSelectionBroker{entries: test.entries}
			runner, _ := New(broker)
			_, runErr := runner.Run(context.Background(), pkg, map[string]any{})
			if runErr == nil || !strings.Contains(runErr.Error(), test.code) {
				t.Fatalf("error = %v, want code %s", runErr, test.code)
			}
		})
	}
}

func TestSaveApplicationFailuresAreExplicitAndReleaseHandle(t *testing.T) {
	tests := []struct {
		name string
		mode string
		code string
	}{
		{name: "load return code", mode: "load", code: "SAVE_LOAD_FAILED"},
		{name: "query return code", mode: "query", code: "SAVE_INVENTORY_QUERY_FAILED"},
		{name: "record count", mode: "count", code: "SAVE_INVENTORY_COUNT_INVALID"},
		{name: "read return code", mode: "read", code: "SAVE_INVENTORY_READ_FAILED"},
		{name: "count changed", mode: "changed", code: "SAVE_INVENTORY_CHANGED"},
	}
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker := &saveFallbackBroker{}
			state := &failureNativeState{mode: test.mode}
			runner, _ := New(broker)
			runner.nativeBackend = failureNativeBackend{state: state}
			_, runErr := runner.Run(context.Background(), pkg, map[string]any{})
			if runErr == nil || !strings.Contains(runErr.Error(), test.code) {
				t.Fatalf("error = %v, want code %s", runErr, test.code)
			}
			if state.frees != 1 {
				t.Fatalf("free calls = %d, want 1", state.frees)
			}
		})
	}
}

type failingBroker struct {
	calls int
}

func (b *failingBroker) Call(context.Context, string, string, map[string]any) (any, error) {
	b.calls++
	return nil, &BrokerError{
		Code:  "LIMIT_EXCEEDED",
		Cause: errors.New("fixture observer call budget exhausted"),
	}
}

func (b *failingBroker) BlobPath(context.Context, map[string]any) (string, error) {
	b.calls++
	return "", errors.New("fixture blob path failure")
}

func (b *failingBroker) RecordNative(context.Context, NativeRecord) error {
	b.calls++
	return nil
}

func TestNonEligibleBrokerFailureDoesNotFallback(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	broker := &failingBroker{}
	runner, _ := New(broker)
	_, err = runner.Run(context.Background(), pkg, map[string]any{})
	var runError *Error
	if !errors.As(err, &runError) {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if runError.Code != "LIMIT_EXCEEDED" {
		t.Fatalf("code = %q", runError.Code)
	}
	if broker.calls != 1 {
		t.Fatalf("broker calls = %d, want 1 (no fallback)", broker.calls)
	}
}

func TestCallerSuppliedSaveInputFailsBeforeScriptExecution(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	broker := &ambiguousBroker{}
	runner, _ := New(broker)
	_, err = runner.Run(context.Background(), pkg, map[string]any{
		"save": map[string]any{"relative": "caller-selected.save"},
	})
	var runError *Error
	if !errors.As(err, &runError) {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if runError.Code != "SCRIPT_INPUT_INVALID" {
		t.Fatalf("code = %q", runError.Code)
	}
	if len(broker.calls) != 0 {
		t.Fatalf("broker calls = %d, want 0", len(broker.calls))
	}
}

type memoryValidationBroker struct {
	fixtureBroker
	mode string
}

func (b *memoryValidationBroker) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	if namespace == "file" {
		return nil, &BrokerError{
			Code:             "OBSERVER_CALL_FAILED",
			FallbackEligible: true,
			Cause:            errors.New("fixture save source unavailable"),
		}
	}
	switch {
	case b.mode == "unsupported-build" && operation == "modules":
		return map[string]any{
			"process": map[string]any{"imageSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"modules": []any{},
		}, nil
	case b.mode == "missing-module" && operation == "modules":
		return map[string]any{
			"process": map[string]any{"imageSha256": crimsonImageSHA256},
			"modules": []any{},
		}, nil
	case b.mode == "ambiguous-signature" && operation == "scan":
		return map[string]any{"matches": []any{}}, nil
	case b.mode == "invalid-header" && operation == "readBatch":
		reads := arguments["reads"].([]any)
		if len(reads) == 2 {
			return map[string]any{"reads": []any{
				map[string]any{"value": uint64(0)},
				map[string]any{"value": uint64(2049)},
			}}, nil
		}
	}
	return b.fixtureBroker.Call(ctx, namespace, operation, arguments)
}

func TestMemoryValidationFailuresAreVisibleBeforeExplicitSave(t *testing.T) {
	tests := []struct {
		name string
		mode string
		code string
	}{
		{name: "unsupported build", mode: "unsupported-build", code: "UNSUPPORTED_BUILD"},
		{name: "missing module", mode: "missing-module", code: "TARGET_MODULE_NOT_FOUND"},
		{name: "ambiguous signature", mode: "ambiguous-signature", code: "INVENTORY_SIGNATURE_AMBIGUOUS"},
		{name: "invalid header", mode: "invalid-header", code: "INVENTORY_HEADER_INVALID"},
	}
	root, _ := filepath.Abs(filepath.Join("..", "..", "Rules", "CrimsonDesert.exe", "Actions", "inventory"))
	pkg, err := scriptpackage.Load(root, "crimson-desert/inventory")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broker := &memoryValidationBroker{mode: test.mode}
			runner, _ := New(broker)
			_, runErr := runner.Run(context.Background(), pkg, map[string]any{})
			if runErr == nil || !strings.Contains(runErr.Error(), test.code) {
				t.Fatalf("error = %v, want code %s", runErr, test.code)
			}
		})
	}
}
