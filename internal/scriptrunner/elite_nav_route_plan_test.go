package scriptrunner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

func navRoutePlanRaw(event string, route []any) map[string]any {
	return map[string]any{
		"schemaVersion": int64(1),
		"state":         "AVAILABLE",
		"freshness":     "UNKNOWN",
		"source": map[string]any{
			"sourceTimestamp": "2026-08-10T08:00:00Z",
		},
		"data": map[string]any{
			"timestamp": "2026-08-10T08:00:00Z",
			"event":     event,
			"Route":     route,
		},
	}
}

func navRouteSystem(name string, address int64, class string, x float64) map[string]any {
	return map[string]any{
		"StarSystem":    name,
		"SystemAddress": address,
		"StarPos":       []any{x, 2.0, 3.0},
		"StarClass":     class,
	}
}

func runNavRoutePlan(t *testing.T, raw map[string]any, destination string, maxJumps int64) ([]byte, error) {
	t.Helper()
	pkg, err := scriptpackage.Load(eliteActionPackageRoot(t, "nav-route-plan"), "elite-dangerous/nav-route-plan")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := New(&fixtureBroker{})
	if err != nil {
		t.Fatal(err)
	}
	return runner.Run(context.Background(), pkg, map[string]any{
		"raw":                       raw,
		"expectedDestinationSystem": destination,
		"maxJumps":                  maxJumps,
	})
}

func TestEliteNavRoutePlanProducesFrozenOrderedHops(t *testing.T) {
	raw := navRoutePlanRaw("NavRoute", []any{
		navRouteSystem("Origin", 1, "G", 1),
		navRouteSystem("Middle", 2, "K", 4),
		navRouteSystem("Destination", 3, "M", 7),
	})
	output, err := runNavRoutePlan(t, raw, "Destination", 4)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "READY" || result["jumpCount"] != float64(2) || result["routeId"] != "2026-08-10T08:00:00Z:3:2" {
		t.Fatalf("result=%#v", result)
	}
	hops := result["hops"].([]any)
	if len(hops) != 2 || hops[0].(map[string]any)["name"] != "Middle" || hops[1].(map[string]any)["name"] != "Destination" {
		t.Fatalf("hops=%#v", hops)
	}
}

func TestEliteNavRoutePlanRejectsClearedMismatchDuplicateAndLimit(t *testing.T) {
	base := []any{
		navRouteSystem("Origin", 1, "G", 1),
		navRouteSystem("Middle", 2, "K", 4),
		navRouteSystem("Destination", 3, "M", 7),
	}
	tests := []struct {
		name        string
		raw         map[string]any
		destination string
		maxJumps    int64
		wantCode    string
	}{
		{name: "cleared", raw: navRoutePlanRaw("NavRouteClear", nil), destination: "Destination", maxJumps: 4, wantCode: "ED_NAV_ROUTE_CLEARED"},
		{name: "destination mismatch", raw: navRoutePlanRaw("NavRoute", base), destination: "Elsewhere", maxJumps: 4, wantCode: "ED_NAV_ROUTE_DESTINATION_MISMATCH"},
		{name: "jump limit", raw: navRoutePlanRaw("NavRoute", base), destination: "Destination", maxJumps: 1, wantCode: "ED_NAV_ROUTE_JUMP_LIMIT"},
		{name: "duplicate address", raw: navRoutePlanRaw("NavRoute", []any{navRouteSystem("Origin", 1, "G", 1), navRouteSystem("Destination", 1, "M", 7)}), destination: "Destination", maxJumps: 4, wantCode: "ED_NAV_ROUTE_DUPLICATE_SYSTEM"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := runNavRoutePlan(t, test.raw, test.destination, test.maxJumps)
			if err == nil || !strings.Contains(err.Error(), test.wantCode) {
				t.Fatalf("error=%v, want %s", err, test.wantCode)
			}
		})
	}
}
