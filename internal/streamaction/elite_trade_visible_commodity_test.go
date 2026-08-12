package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type tradeVisibleCommodityCaller struct {
	ocr      []json.RawMessage
	cargo    []json.RawMessage
	controls []string
	clicks   []map[string]any
	exits    int
}

func (c *tradeVisibleCommodityCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/commodity-market-header-text-regions":
		if len(c.ocr) == 0 {
			return nil, errors.New("missing Commodity Market header OCR fixture")
		}
		value := c.ocr[0]
		c.ocr = c.ocr[1:]
		return value, nil
	case "elite-dangerous/commodity-market-text-regions":
		if len(c.ocr) == 0 {
			return nil, errors.New("missing Commodity Market OCR fixture")
		}
		value := c.ocr[0]
		c.ocr = c.ocr[1:]
		return value, nil
	case "elite-dangerous/filesystem/cargo":
		if len(c.cargo) == 0 {
			return nil, errors.New("missing Cargo fixture")
		}
		value := c.cargo[0]
		c.cargo = c.cargo[1:]
		return value, nil
	case "elite-dangerous/ui-control":
		control, ok := inputs["control"].(string)
		if !ok {
			return nil, errors.New("UI control is not a string")
		}
		c.controls = append(c.controls, control)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/pointer-click":
		c.clicks = append(c.clicks, inputs)
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/exit-commodity-market":
		c.exits++
		if inputs["dialogMayBeOpen"] != false {
			return nil, errors.New("normal cleanup unexpectedly allows an open dialog")
		}
		return json.RawMessage(`{"schemaVersion":1,"backCount":2,"settleMs":1800}`), nil
	default:
		return nil, errors.New("unexpected trade-visible-commodity child Action: " + id)
	}
}

func loadEliteTradeVisibleCommodityPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "trade-visible-commodity"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func commodityRegion(text string, x, y int) map[string]any {
	return map[string]any{
		"detectionConfidence": 0.94, "recognitionConfidence": 0.98, "text": text,
		"referencePoints": []any{
			map[string]any{"x": x, "y": y}, map[string]any{"x": x + 180, "y": y},
			map[string]any{"x": x + 180, "y": y + 30}, map[string]any{"x": x, "y": y + 30},
		},
	}
}

func commodityOCR(texts ...map[string]any) json.RawMessage {
	regions := make([]any, len(texts))
	for index := range texts {
		regions[index] = texts[index]
	}
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "regions": regions})
	return value
}

func commodityMarketOCR(mode, commodity string) json.RawMessage {
	return commodityOCR(
		commodityRegion("COMMODITIES MARKET", 250, 170),
		commodityRegion("CREON'S STANDING", 250, 205),
		commodityRegion(mode, 270, 235),
		commodityRegion(commodity, 400, 325),
	)
}

func commodityDialogOCR(operation, commodity string) json.RawMessage {
	return commodityOCR(
		commodityRegion(operation+" COMMODITY", 550, 250),
		commodityRegion(commodity, 620, 330),
	)
}

func cargoFixtureWithFreshness(timestamp, commodity string, count int, freshness string) json.RawMessage {
	inventory := []any{}
	if count > 0 {
		inventory = append(inventory, map[string]any{"Name": "fixture", "Name_Localised": commodity, "Count": count, "Stolen": 0})
	}
	value, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": "AVAILABLE", "freshness": freshness,
		"source": map[string]any{},
		"data":   map[string]any{"timestamp": timestamp, "event": "Cargo", "Vessel": "Ship", "Count": count, "Inventory": inventory},
	})
	return value
}

func cargoFixture(timestamp, commodity string, count int) json.RawMessage {
	return cargoFixtureWithFreshness(timestamp, commodity, count, "CURRENT")
}

func TestEliteTradeVisibleCommodityBuysExactQuantityAndRequiresNewCargo(t *testing.T) {
	caller := &tradeVisibleCommodityCaller{
		ocr: []json.RawMessage{
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), commodityOCR(commodityRegion("HYDROGEN FUEL", 400, 325)),
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), commodityOCR(commodityRegion("HYDROGEN FUEL", 400, 325)),
			commodityDialogOCR("BUY", "HYDROGEN FUEL"), commodityDialogOCR("BUY", "HYDROGEN FUEL"),
			commodityOCR(), commodityOCR(),
		},
		cargo: []json.RawMessage{
			cargoFixtureWithFreshness("2026-08-12T09:41:00Z", "Hydrogen Fuel", 1, "UNKNOWN"),
			cargoFixture("2026-08-12T09:42:00Z", "Hydrogen Fuel", 4),
		},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteTradeVisibleCommodityPackage(t), map[string]any{
			"operation": "BUY", "commodityName": "Hydrogen Fuel", "quantity": 3.0, "stationName": "Creon's Standing",
		}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"RIGHT", "RIGHT", "RIGHT", "DOWN", "SELECT"}
	if !equalStrings(caller.controls, want) {
		t.Fatalf("controls=%v want=%v", caller.controls, want)
	}
	if len(caller.clicks) != 1 || caller.clicks[0]["x"] != int64(490) || caller.clicks[0]["y"] != int64(340) {
		t.Fatalf("clicks=%v", caller.clicks)
	}
	if caller.exits != 1 {
		t.Fatalf("exits=%d want=1", caller.exits)
	}
	if !contains(string(output), `"beforeCount":1`) || !contains(string(output), `"afterCount":4`) ||
		!contains(string(output), `"commodityMarketAbsent":true`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteTradeVisibleCommodityFailsBeforeInputOnAmbiguousExactRows(t *testing.T) {
	duplicate := commodityOCR(
		commodityRegion("COMMODITIES MARKET", 250, 170), commodityRegion("CREON'S STANDING", 250, 205),
		commodityRegion("BUY FROM MARKET", 270, 235), commodityRegion("GOLD", 400, 325), commodityRegion("GOLD", 400, 365),
	)
	caller := &tradeVisibleCommodityCaller{
		ocr: []json.RawMessage{
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
			commodityMarketOCR("BUY FROM MARKET", "IGNORED"), duplicate,
		},
		cargo: []json.RawMessage{cargoFixture("2026-08-12T09:41:00Z", "Gold", 0)},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteTradeVisibleCommodityPackage(t), map[string]any{
			"operation": "BUY", "commodityName": "Gold", "quantity": 1, "stationName": "Creon's Standing",
		}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "one exact visible commodity") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || len(caller.clicks) != 0 {
		t.Fatalf("controls=%v clicks=%v", caller.controls, caller.clicks)
	}
}

func TestEliteTradeVisibleCommodityRejectsInsufficientSellCargo(t *testing.T) {
	caller := &tradeVisibleCommodityCaller{cargo: []json.RawMessage{cargoFixture("2026-08-12T09:41:00Z", "Hydrogen Fuel", 1)}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteTradeVisibleCommodityPackage(t), map[string]any{
			"operation": "SELL", "commodityName": "Hydrogen Fuel", "quantity": 2, "stationName": "Creon's Standing",
		}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "does not contain enough") {
		t.Fatalf("error=%v", err)
	}
}

type exitCommodityMarketCaller struct {
	controls []string
}

func (c *exitCommodityMarketCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/ui-control":
		c.controls = append(c.controls, inputs["control"].(string))
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/commodity-market-header-text-regions":
		return commodityOCR(commodityRegion("COMMODITIES MARKET", 250, 170)), nil
	default:
		return nil, errors.New("unexpected exit-commodity-market child Action: " + id)
	}
}

func TestEliteExitCommodityMarketAllowsOneDialogBackBeforeLeavingServices(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "exit-commodity-market"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	caller := &exitCommodityMarketCaller{}
	output, err := (Runner{Sleep: immediateSleep}).Run(context.Background(), pkg, map[string]any{"dialogMayBeOpen": true}, caller, &fixtureReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.controls, []string{"BACK", "BACK", "BACK"}) || !contains(string(output), `"backCount":3`) {
		t.Fatalf("controls=%v output=%s", caller.controls, output)
	}
}
