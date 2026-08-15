package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type setCommodityMarketViewCaller struct {
	controls []string
	failAt   int
}

func (c *setCommodityMarketViewCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	if id != "elite-dangerous/ui-control" {
		return nil, errors.New("unexpected set-commodity-market-view child Action: " + id)
	}
	control, ok := inputs["control"].(string)
	if !ok {
		return nil, errors.New("UI control is not a string")
	}
	if c.failAt > 0 && len(c.controls)+1 == c.failAt {
		return nil, errors.New("injected UI control failure")
	}
	c.controls = append(c.controls, control)
	return json.RawMessage(`{"schemaVersion":1}`), nil
}

func loadEliteSetCommodityMarketViewPackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "set-commodity-market-view"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func repeatedControls(control string, count int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = control
	}
	return result
}

func appendControls(parts ...[]string) []string {
	result := []string{}
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

func TestEliteSetCommodityMarketViewReplaysExactBuyAllGoodsStructure(t *testing.T) {
	caller := &setCommodityMarketViewCaller{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSetCommodityMarketViewPackage(t), map[string]any{"profile": "BUY_ALL_GOODS"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := appendControls(
		repeatedControls("DOWN", 3), []string{"SELECT"}, repeatedControls("DOWN", 20), repeatedControls("RIGHT", 5),
		[]string{"SELECT"}, repeatedControls("LEFT", 5), []string{"RIGHT", "SELECT"}, repeatedControls("UP", 3), []string{"SELECT", "RIGHT"},
	)
	if !equalStrings(caller.controls, want) || len(caller.controls) != 42 {
		t.Fatalf("controls=%v len=%d", caller.controls, len(caller.controls))
	}
	if !contains(string(output), `"profile":"BUY_ALL_GOODS"`) || !contains(string(output), `"controlCount":42`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSetCommodityMarketViewReplaysExactSellSingleCargoStructure(t *testing.T) {
	caller := &setCommodityMarketViewCaller{}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSetCommodityMarketViewPackage(t), map[string]any{"profile": "SELL_SINGLE_CARGO"}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := appendControls(
		repeatedControls("DOWN", 3), []string{"SELECT"}, repeatedControls("DOWN", 20), repeatedControls("RIGHT", 2), []string{"SELECT"},
		repeatedControls("UP", 10), []string{"RIGHT", "SELECT"}, repeatedControls("DOWN", 15), repeatedControls("LEFT", 3),
		[]string{"RIGHT", "SELECT"}, repeatedControls("UP", 2), []string{"SELECT", "RIGHT"},
	)
	if !equalStrings(caller.controls, want) || len(caller.controls) != 63 {
		t.Fatalf("controls=%v len=%d", caller.controls, len(caller.controls))
	}
	if !contains(string(output), `"profile":"SELL_SINGLE_CARGO"`) || !contains(string(output), `"controlCount":63`) {
		t.Fatalf("output=%s", output)
	}
}

func TestEliteSetCommodityMarketViewStopsAfterFirstInputFailure(t *testing.T) {
	caller := &setCommodityMarketViewCaller{failAt: 9}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), loadEliteSetCommodityMarketViewPackage(t), map[string]any{"profile": "BUY_ALL_GOODS"}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "injected UI control failure") {
		t.Fatalf("error=%v", err)
	}
	if len(caller.controls) != 8 {
		t.Fatalf("controls after failure=%v", caller.controls)
	}
}
