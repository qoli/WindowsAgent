package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type plotRouteCaller struct {
	search       []json.RawMessage
	selected     []json.RawMessage
	routeReady   bool
	controls     []string
	textKeys     []string
	pointer      [][2]int64
	holdMS       []int64
	navPlanCalls int
}

func (c *plotRouteCaller) Call(_ context.Context, id string, inputs map[string]any) (json.RawMessage, error) {
	switch id {
	case "elite-dangerous/filesystem/nav-route":
		return json.RawMessage(`{"state":"AVAILABLE"}`), nil
	case "elite-dangerous/nav-route-plan":
		c.navPlanCalls++
		if !c.routeReady {
			return nil, errors.New("ED_NAV_ROUTE_DESTINATION_MISMATCH")
		}
		return json.RawMessage(`{"routeId":"2026-08-12T00:00:00Z:178:1","jumpCount":1,"freshness":"CURRENT"}`), nil
	case "elite-dangerous/galaxy-map-search-text-regions":
		if len(c.search) == 0 {
			return nil, errors.New("unexpected Galaxy Map search observation")
		}
		value := c.search[0]
		c.search = c.search[1:]
		return value, nil
	case "elite-dangerous/galaxy-map-system-info-text-regions":
		if len(c.selected) == 0 {
			return nil, errors.New("unexpected Galaxy Map System observation")
		}
		value := c.selected[0]
		c.selected = c.selected[1:]
		return value, nil
	case "elite-dangerous/ui-control":
		c.controls = append(c.controls, inputs["control"].(string))
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/text-entry-key":
		c.textKeys = append(c.textKeys, inputs["key"].(string))
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/pointer-click":
		c.pointer = append(c.pointer, [2]int64{inputs["x"].(int64), inputs["y"].(int64)})
		return json.RawMessage(`{"schemaVersion":1}`), nil
	case "elite-dangerous/ui-select-hold":
		c.holdMS = append(c.holdMS, inputs["holdMs"].(int64))
		c.routeReady = true
		return json.RawMessage(`{"schemaVersion":1}`), nil
	default:
		return nil, errors.New("unexpected plot-route child Action: " + id)
	}
}

func galaxyOCR(regions ...map[string]any) json.RawMessage {
	values := make([]any, 0, len(regions))
	for _, region := range regions {
		values = append(values, region)
	}
	value, _ := json.Marshal(map[string]any{"regions": values})
	return value
}

func galaxyRegion(text string, y int) map[string]any {
	return map[string]any{
		"detectionConfidence": 0.95, "recognitionConfidence": 0.96, "text": text,
		"referencePoints": []any{
			map[string]any{"x": 760, "y": y}, map[string]any{"x": 940, "y": y},
			map[string]any{"x": 940, "y": y + 30}, map[string]any{"x": 760, "y": y + 30},
		},
	}
}

func plotRoutePackage(t *testing.T) *Package {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "plot-route-to-system"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestElitePlotRouteRequiresExactSuggestionAndVerifiesNavRoute(t *testing.T) {
	absent := galaxyOCR()
	mapOnly := galaxyOCR(galaxyRegion("GALAXY MAP I REALISTIC", 50))
	withSuggestion := galaxyOCR(
		galaxyRegion("GALAXY MAP I REALISTIC", 50),
		galaxyRegion("LHS 178", 165),
		galaxyRegion("LHS 1788", 200),
	)
	withSuggestionShifted := galaxyOCR(
		galaxyRegion("GALAXY MAP I REALISTIC", 50),
		galaxyRegion("LHS 178", 166),
		galaxyRegion("LHS 1788", 201),
	)
	selected := galaxyOCR(galaxyRegion("LHS 178", 220))
	caller := &plotRouteCaller{
		search:   []json.RawMessage{absent, absent, mapOnly, mapOnly, withSuggestion, withSuggestionShifted, absent, absent},
		selected: []json.RawMessage{selected, selected},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{"targetSystem": "LHS 178", "maxJumps": int64(1)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"PLOTTED"`) || !contains(string(output), `"routeId":"2026-08-12T00:00:00Z:178:1"`) {
		t.Fatalf("output=%s", output)
	}
	if !equalStrings(caller.controls, []string{"OPEN_GALAXY_MAP", "OPEN_GALAXY_MAP"}) {
		t.Fatalf("controls=%v", caller.controls)
	}
	wantTextKeys := make([]string, 96, 103)
	for index := range wantTextKeys {
		wantTextKeys[index] = "BACKSPACE"
	}
	wantTextKeys = append(wantTextKeys, "L", "H", "S", "SPACE", "1", "7", "8")
	if !equalStrings(caller.textKeys, wantTextKeys) {
		t.Fatalf("textKeys=%v", caller.textKeys)
	}
	if len(caller.pointer) != 2 || caller.pointer[0] != [2]int64{900, 135} || caller.pointer[1] != [2]int64{850, 181} {
		t.Fatalf("pointer=%v", caller.pointer)
	}
	if len(caller.holdMS) != 1 || caller.holdMS[0] != 1000 || caller.navPlanCalls != 2 {
		t.Fatalf("holdMS=%v navPlanCalls=%d", caller.holdMS, caller.navPlanCalls)
	}
}

func TestElitePlotRouteResumesObservedOpenMapAndStillRestoresView(t *testing.T) {
	mapOnly := galaxyOCR(galaxyRegion("GALAXYEMAPTPPREALISTIC", 50))
	withSuggestion := galaxyOCR(galaxyRegion("GALAXY MAP I REALISTIC", 50), galaxyRegion("LHS 178", 165))
	selected := galaxyOCR(galaxyRegion("LHS 178", 220))
	absent := galaxyOCR()
	caller := &plotRouteCaller{
		search:   []json.RawMessage{mapOnly, mapOnly, withSuggestion, withSuggestion, absent, absent},
		selected: []json.RawMessage{selected, selected},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{"targetSystem": "LHS 178", "maxJumps": int64(1)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"PLOTTED"`) || !contains(string(output), `"openedMap":false`) || !equalStrings(caller.controls, []string{"OPEN_GALAXY_MAP"}) {
		t.Fatalf("output=%s controls=%v", output, caller.controls)
	}
}

func TestElitePlotRouteRejectsTitleWordsWithoutGalaxyPrefix(t *testing.T) {
	notMap := galaxyOCR(galaxyRegion("SEARCH MAP REALISTIC", 50))
	caller := &plotRouteCaller{search: []json.RawMessage{notMap, notMap}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{"targetSystem": "LHS 178", "maxJumps": int64(1)}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "unexpected Galaxy Map search observation") {
		t.Fatalf("error=%v", err)
	}
	if !equalStrings(caller.controls, []string{"OPEN_GALAXY_MAP", "OPEN_GALAXY_MAP"}) {
		t.Fatalf("controls=%v", caller.controls)
	}
}

func TestElitePlotRouteAcceptsOnlyExistingExactRouteWithoutOpeningMap(t *testing.T) {
	caller := &plotRouteCaller{routeReady: true}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{"targetSystem": "LHS 178", "maxJumps": int64(1)}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"EXISTING"`) || len(caller.controls) != 0 || len(caller.pointer) != 0 || len(caller.textKeys) != 0 {
		t.Fatalf("output=%s controls=%v pointer=%v text=%v", output, caller.controls, caller.pointer, caller.textKeys)
	}
}

func TestElitePlotRouteRefreshesExistingRouteContextWithoutReplotting(t *testing.T) {
	absent := galaxyOCR()
	mapOnly := galaxyOCR(galaxyRegion("GALAXY MAP I REALISTIC", 50))
	caller := &plotRouteCaller{
		routeReady: true,
		search:     []json.RawMessage{absent, absent, mapOnly, mapOnly, absent, absent},
	}
	output, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{
			"targetSystem": "LHS 178", "maxJumps": int64(1), "refreshExistingContext": true,
		}, caller, &fixtureReporter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(output), `"result":"REFRESHED"`) || !contains(string(output), `"routeId":"2026-08-12T00:00:00Z:178:1"`) {
		t.Fatalf("output=%s", output)
	}
	if !equalStrings(caller.controls, []string{"OPEN_GALAXY_MAP", "OPEN_GALAXY_MAP"}) || len(caller.pointer) != 0 || len(caller.textKeys) != 0 || len(caller.holdMS) != 0 || caller.navPlanCalls != 2 {
		t.Fatalf("controls=%v pointer=%v text=%v holdMS=%v navPlanCalls=%d", caller.controls, caller.pointer, caller.textKeys, caller.holdMS, caller.navPlanCalls)
	}
}

func TestElitePlotRouteRejectsPartialSuggestionAndClosesOwnedMap(t *testing.T) {
	absent := galaxyOCR()
	mapOnly := galaxyOCR(galaxyRegion("GALAXY MAP I REALISTIC", 50))
	partial := galaxyOCR(galaxyRegion("GALAXY MAP I REALISTIC", 50), galaxyRegion("LHS 1788", 165))
	caller := &plotRouteCaller{search: []json.RawMessage{absent, absent, mapOnly, mapOnly, partial, partial, partial, partial, partial, partial}}
	_, err := (Runner{Sleep: immediateSleep}).Run(
		context.Background(), plotRoutePackage(t), map[string]any{"targetSystem": "LHS 178", "maxJumps": int64(1)}, caller, &fixtureReporter{},
	)
	if err == nil || !contains(err.Error(), "complete exact System suggestion") {
		t.Fatalf("error=%v", err)
	}
	if !equalStrings(caller.controls, []string{"OPEN_GALAXY_MAP", "OPEN_GALAXY_MAP"}) || len(caller.holdMS) != 0 {
		t.Fatalf("controls=%v holdMS=%v", caller.controls, caller.holdMS)
	}
}
