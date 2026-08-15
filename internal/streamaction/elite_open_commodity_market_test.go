package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type openCommodityMarketCaller struct {
	docked              []json.RawMessage
	header              []json.RawMessage
	controls            []string
	clicks              []map[string]any
	views               []string
	viewOverride        json.RawMessage
	exits               int
	exitDialogMayBeOpen []bool
}

func (c *openCommodityMarketCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/docked-cockpit-menu-text-regions":
		if len(c.docked) == 0 {
			return nil, errors.New("missing docked menu OCR fixture")
		}
		value := c.docked[0]
		c.docked = c.docked[1:]
		return value, nil
	case "elite-dangerous/commodity-market-header-text-regions":
		if len(c.header) == 0 {
			return nil, errors.New("missing market header OCR fixture")
		}
		value := c.header[0]
		c.header = c.header[1:]
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
	case "elite-dangerous/set-commodity-market-view":
		profile, ok := inputs["profile"].(string)
		if !ok {
			return nil, errors.New("market view profile is not a string")
		}
		c.views = append(c.views, profile)
		if c.viewOverride != nil {
			return c.viewOverride, nil
		}
		controlCount := 42
		if profile == "SELL_SINGLE_CARGO" {
			controlCount = 63
		}
		value, _ := json.Marshal(map[string]any{
			"schemaVersion": 1, "task": "SET_COMMODITY_MARKET_VIEW", "completed": true,
			"profile": profile, "filterReplayCompleted": true, "listFocusCommanded": true, "controlCount": controlCount,
		})
		return value, nil
	case "elite-dangerous/exit-commodity-market":
		c.exits++
		mayBeOpen, _ := inputs["dialogMayBeOpen"].(bool)
		c.exitDialogMayBeOpen = append(c.exitDialogMayBeOpen, mayBeOpen)
		return json.RawMessage(`{"schemaVersion":1,"backCount":2,"settleMs":1800}`), nil
	default:
		return nil, errors.New("unexpected open-commodity-market child Action: " + id)
	}
}

func TestEliteOpenCommodityMarketNormalizesBuyAllGoodsAndReconfirmsMode(t *testing.T) {
	caller := &openCommodityMarketCaller{
		docked: []json.RawMessage{
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
		},
		header: []json.RawMessage{
			marketHeaderOCR("SHAW STATION", "SELL TO MARKET"),
			marketHeaderOCR("SHAW STATION", "SELL TO MARKET"),
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
		},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteOpenCommodityMarketPackage(t), map[string]any{
			"operation": "BUY", "stationName": "Shaw Station",
		}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(caller.views, []string{"BUY_ALL_GOODS"}) || !contains(string(output), `"viewControlCount":42`) {
		t.Fatalf("views=%v output=%s", caller.views, output)
	}
}

func TestEliteOpenCommodityMarketRejectsInvalidMechanicalViewResultAndCleansUp(t *testing.T) {
	caller := &openCommodityMarketCaller{
		docked: []json.RawMessage{
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
		},
		header: []json.RawMessage{
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
		},
		viewOverride: json.RawMessage(`{"schemaVersion":1,"task":"SET_COMMODITY_MARKET_VIEW","completed":true,"profile":"BUY_ALL_GOODS","filterReplayCompleted":true,"listFocusCommanded":true,"controlCount":41}`),
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteOpenCommodityMarketPackage(t), map[string]any{
			"operation": "BUY", "stationName": "Shaw Station",
		}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "invalid mechanical replay") {
		t.Fatalf("error=%v", err)
	}
	if caller.exits != 1 || len(caller.exitDialogMayBeOpen) != 1 || !caller.exitDialogMayBeOpen[0] {
		t.Fatalf("exits=%d exitDialogMayBeOpen=%v", caller.exits, caller.exitDialogMayBeOpen)
	}
}

func loadEliteOpenCommodityMarketPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "open-commodity-market"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func dockedMenuOCR(labels ...string) json.RawMessage {
	regions := make([]any, 0, len(labels))
	for index, label := range labels {
		regions = append(regions, commodityRegion(label, 900, 760+index*40))
	}
	value, _ := json.Marshal(map[string]any{"schemaVersion": 1, "regions": regions})
	return value
}

func marketHeaderOCR(station, mode string) json.RawMessage {
	return commodityOCR(
		commodityRegion("COMMODITIES MARKET", 140, 70),
		commodityRegion(station, 140, 105),
		commodityRegion(mode, 250, 180),
	)
}

func TestEliteOpenCommodityMarketOpensAndSwitchesToSellWithOCRPostcondition(t *testing.T) {
	caller := &openCommodityMarketCaller{
		docked: []json.RawMessage{
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
		},
		header: []json.RawMessage{
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
			marketHeaderOCR("SHAW STATION", "BUY FROM MARKET"),
			marketHeaderOCR("SHAW STATION", "SELL TO MARKET"),
			marketHeaderOCR("SHAW STATION", "SELL TO MARKET"),
		},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteOpenCommodityMarketPackage(t), map[string]any{
			"operation": "SELL", "stationName": "Shaw Station",
		}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantControls := []string{"DOWN", "DOWN", "DOWN", "DOWN", "UP", "UP", "SELECT", "SELECT"}
	if !equalStrings(caller.controls, wantControls) {
		t.Fatalf("controls=%v want=%v", caller.controls, wantControls)
	}
	if len(caller.clicks) != 1 || caller.clicks[0]["x"] != int64(395) || caller.clicks[0]["y"] != int64(704) {
		t.Fatalf("clicks=%v", caller.clicks)
	}
	if !equalStrings(caller.views, []string{"SELL_SINGLE_CARGO"}) {
		t.Fatalf("views=%v", caller.views)
	}
	if caller.exits != 0 {
		t.Fatalf("failure cleanup executed on success: exits=%d", caller.exits)
	}
	if !contains(string(output), `"initialMode":"BUY"`) || !contains(string(output), `"modeConfirmed":true`) ||
		!contains(string(output), `"marketViewProfile":"SELL_SINGLE_CARGO"`) || !contains(string(output), `"viewControlCount":63`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteOpenCommodityMarketDoesNotInjectInputWithoutStableDockedMenu(t *testing.T) {
	caller := &openCommodityMarketCaller{docked: []json.RawMessage{
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
		dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH"),
	}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteOpenCommodityMarketPackage(t), map[string]any{
			"operation": "BUY", "stationName": "Shaw Station",
		}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "complete docked cockpit menu") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 0 || len(caller.clicks) != 0 || caller.exits != 0 {
		t.Fatalf("controls=%v clicks=%v exits=%d", caller.controls, caller.clicks, caller.exits)
	}
}

func TestEliteOpenCommodityMarketRejectsWrongStationAfterOpening(t *testing.T) {
	caller := &openCommodityMarketCaller{
		docked: []json.RawMessage{
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
			dockedMenuOCR("STARPORT SERVICES", "AUTO LAUNCH", "DISEMBARK"),
		},
		header: []json.RawMessage{
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
			marketHeaderOCR("OTHER STATION", "BUY FROM MARKET"),
		},
	}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteOpenCommodityMarketPackage(t), map[string]any{
			"operation": "BUY", "stationName": "Shaw Station",
		}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "exact Station and mode") {
		t.Fatalf("error=%v", err)
	}
	// Cleanup is registered after the docked cockpit menu was proven and must
	// restore the cockpit even when market-header validation fails.
	if caller.exits != 1 {
		t.Fatalf("exits=%d want=1", caller.exits)
	}
}
