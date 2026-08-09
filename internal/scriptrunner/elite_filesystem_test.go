package scriptrunner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

type eliteFilesystemBroker struct {
	result    map[string]any
	operation string
	arguments map[string]any
}

func (b *eliteFilesystemBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	if namespace != "file" {
		return nil, errors.New("unexpected observer namespace")
	}
	b.operation = operation
	b.arguments = arguments
	return b.result, nil
}

func (*eliteFilesystemBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected blob path request")
}

func (*eliteFilesystemBroker) RecordNative(context.Context, NativeRecord) error {
	return errors.New("unexpected native record")
}

func eliteFilesystemPackageRoot(t *testing.T, capabilityID string) string {
	t.Helper()
	slug := strings.TrimPrefix(capabilityID, "elite-dangerous/filesystem/")
	if slug == capabilityID || slug == "" {
		t.Fatalf("invalid filesystem capability ID %q", capabilityID)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "Rules", "EliteDangerous64.exe", "Actions", "filesystem-"+slug))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func availableFilesystemResult(event string) map[string]any {
	return map[string]any{
		"path":                 map[string]any{"root": "elite-dangerous-journal", "relative": "fixture.json"},
		"exists":               true,
		"observedAt":           "2026-08-09T05:00:52Z",
		"modifiedAt":           "2026-08-09T05:00:52Z",
		"modifiedAgeMs":        int64(0),
		"sizeBytes":            int64(128),
		"sourceTimestamp":      "2026-08-09T05:00:51Z",
		"sourceTimestampAgeMs": int64(1000),
		"data": map[string]any{
			"timestamp":      "2026-08-09T05:00:51Z",
			"event":          event,
			"SecretTopLevel": "must-not-escape",
		},
	}
}

func TestEliteFilesystemActionsBindIdentityToOneDeclaredFile(t *testing.T) {
	tests := []struct {
		id         string
		file       string
		event      string
		updateMode string
	}{
		{id: "elite-dangerous/filesystem/status", file: "Status.json", event: "Status", updateMode: "PERIODIC_5S"},
		{id: "elite-dangerous/filesystem/cargo", file: "Cargo.json", event: "Cargo", updateMode: "EVENT_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/ship-locker", file: "ShipLocker.json", event: "ShipLocker", updateMode: "EVENT_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/backpack", file: "Backpack.json", event: "Backpack", updateMode: "EVENT_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/nav-route", file: "NavRoute.json", event: "NavRouteClear", updateMode: "ROUTE_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/modules-info", file: "ModulesInfo.json", event: "ModuleInfo", updateMode: "INTERACTION_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/market", file: "Market.json", event: "Market", updateMode: "INTERACTION_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/outfitting", file: "Outfitting.json", event: "Outfitting", updateMode: "INTERACTION_SNAPSHOT"},
		{id: "elite-dangerous/filesystem/shipyard", file: "Shipyard.json", event: "Shipyard", updateMode: "INTERACTION_SNAPSHOT"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			pkg, err := scriptpackage.Load(eliteFilesystemPackageRoot(t, test.id), test.id)
			if err != nil {
				t.Fatal(err)
			}
			broker := &eliteFilesystemBroker{result: availableFilesystemResult(test.event)}
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
			if result["state"] != "AVAILABLE" || result["freshness"] != "CURRENT" {
				t.Fatalf("result = %#v", result)
			}
			source := result["source"].(map[string]any)
			if source["capabilityId"] != test.id || source["file"] != test.file || source["updateMode"] != test.updateMode {
				t.Fatalf("source = %#v", source)
			}
			data := result["data"].(map[string]any)
			if _, leaked := data["SecretTopLevel"]; leaked {
				t.Fatalf("undeclared top-level field leaked: %#v", data)
			}
			if broker.operation != "readJson" {
				t.Fatalf("operation = %q", broker.operation)
			}
			path := broker.arguments["path"].(map[string]any)
			if path["root"] != "elite-dangerous-journal" || path["relative"] != test.file {
				t.Fatalf("path = %#v", path)
			}
		})
	}
}

func TestEliteFilesystemActionReturnsExplicitAbsentState(t *testing.T) {
	capabilityID := "elite-dangerous/filesystem/market"
	pkg, err := scriptpackage.Load(eliteFilesystemPackageRoot(t, capabilityID), capabilityID)
	if err != nil {
		t.Fatal(err)
	}
	broker := &eliteFilesystemBroker{result: map[string]any{
		"path":       map[string]any{"root": "elite-dangerous-journal", "relative": "Market.json"},
		"exists":     false,
		"observedAt": "2026-08-09T05:00:52Z",
	}}
	runner, _ := New(broker)
	output, err := runner.Run(context.Background(), pkg, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "ABSENT" || result["freshness"] != "UNKNOWN" || result["data"] != nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestEliteFilesystemActionsApplySourceSpecificFreshness(t *testing.T) {
	tests := []struct {
		id        string
		event     string
		ageMs     int64
		freshness string
	}{
		{id: "elite-dangerous/filesystem/status", event: "Status", ageMs: 15001, freshness: "STALE"},
		{id: "elite-dangerous/filesystem/cargo", event: "Cargo", ageMs: 15001, freshness: "UNKNOWN"},
		{id: "elite-dangerous/filesystem/market", event: "Market", ageMs: 900001, freshness: "STALE"},
		{id: "elite-dangerous/filesystem/status", event: "Status", ageMs: -1, freshness: "UNKNOWN"},
	}
	for _, test := range tests {
		t.Run(test.id+"/"+test.freshness, func(t *testing.T) {
			pkg, err := scriptpackage.Load(eliteFilesystemPackageRoot(t, test.id), test.id)
			if err != nil {
				t.Fatal(err)
			}
			result := availableFilesystemResult(test.event)
			result["sourceTimestampAgeMs"] = test.ageMs
			runner, _ := New(&eliteFilesystemBroker{result: result})
			output, err := runner.Run(context.Background(), pkg, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(output, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["freshness"] != test.freshness {
				t.Fatalf("freshness = %v, want %s", decoded["freshness"], test.freshness)
			}
		})
	}
}

func TestEliteFilesystemActionRejectsWrongEventAndMissingTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(map[string]any)
		wantError string
	}{
		{
			name: "wrong event",
			mutate: func(result map[string]any) {
				result["data"].(map[string]any)["event"] = "Cargo"
			},
			wantError: "ED_FILESYSTEM_EVENT_INVALID",
		},
		{
			name: "missing timestamp",
			mutate: func(result map[string]any) {
				delete(result["data"].(map[string]any), "timestamp")
				result["sourceTimestamp"] = nil
				result["sourceTimestampAgeMs"] = nil
			},
			wantError: "ED_FILESYSTEM_TIMESTAMP_INVALID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capabilityID := "elite-dangerous/filesystem/status"
			pkg, err := scriptpackage.Load(eliteFilesystemPackageRoot(t, capabilityID), capabilityID)
			if err != nil {
				t.Fatal(err)
			}
			result := availableFilesystemResult("Status")
			test.mutate(result)
			runner, _ := New(&eliteFilesystemBroker{result: result})
			_, err = runner.Run(context.Background(), pkg, map[string]any{})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %s", err, test.wantError)
			}
		})
	}
}
