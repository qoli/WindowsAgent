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

type shipStatusBroker struct {
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
			"x": int64(682), "y": int64(771), "w": int64(96), "h": int64(96),
		},
		"physicalRegion": map[string]any{
			"left": int64(1364), "top": int64(1542), "width": int64(192), "height": int64(192),
		},
		"image": map[string]any{
			"width": int64(96), "height": int64(96),
			"encoding": "rgb24-packed", "pixels": b.pixels,
		},
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

func flightStatusPackageRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "flight-status"))
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
		{12, "RESHAGINGORT", 0.75576, "FSD_CHARGING"},
		{13, "PRESSTO ABORT", 0.92825, "FSD_CHARGING"},
		{14, "ALIGN WITH TARGET DESTINATION", 0.990222, "FSD_ALIGNMENT_REQUIRED"},
		{15, "ALIGN WITH TARGET DESTINATION", 0.987779, "FSD_ALIGNMENT_REQUIRED"},
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

func TestEliteFlightStatusPackageKeepsLowConfidenceAndAmbiguousTextUnknown(t *testing.T) {
	for _, test := range []struct {
		name       string
		text       string
		confidence float64
	}{
		{name: "below confidence threshold", text: "AUTO DOCK IN PROGRESS", confidence: 0.299},
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

func TestEliteCompassPackageReportsZeroDistanceAndUndefinedAngleAtCenter(t *testing.T) {
	pixels := make([]any, 96*96)
	for index := range pixels {
		pixels[index] = uint32(0)
	}
	for index := 0; index < 200; index++ {
		pixels[index] = uint32(0xFF7700)
	}
	for y := 47; y < 50; y++ {
		for x := 47; x < 50; x++ {
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
	if target["detected"] != true || target["offsetX"] != float64(0) || target["offsetY"] != float64(0) ||
		target["screenAngleDegrees"] != nil || target["centerDistancePixels"] != float64(0) || zone["inside"] != true {
		t.Fatalf("target = %#v", target)
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
