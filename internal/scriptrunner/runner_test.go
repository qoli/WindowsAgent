package scriptrunner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

const crimsonImageSHA256 = "d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f"

type fixtureBroker struct {
	calls []string
}

func (b *fixtureBroker) Call(_ context.Context, namespace, operation string, arguments map[string]any) (any, error) {
	b.calls = append(b.calls, namespace+"."+operation)
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
	root, err := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	broker := &fixtureBroker{}
	runner, err := New(broker)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runner.Run(context.Background(), pkg, nil)
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
}

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
	root, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&ambiguousBroker{})
	_, err = runner.Run(context.Background(), pkg, map[string]any{
		"save": map[string]any{"root": "crimson-desert-saves", "relative": "slot/save.save"},
	})
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
		return map[string]any{"matches": []any{}}, nil
	}
	if operation == "decode" {
		b.calls = append(b.calls, namespace+"."+operation)
		return map[string]any{
			"decoder":    "crimson-rs/inventory@bb730180",
			"modifiedAt": "2026-07-27T13:42:00Z",
			"value": map[string]any{
				"recordCount": int64(2),
				"items": []any{
					map[string]any{
						"slot":         int64(4),
						"itemId":       int64(11),
						"quantity":     int64(77789),
						"inventoryKey": int64(1),
						"instanceId":   int64(9001),
					},
					map[string]any{
						"slot":         int64(5),
						"itemId":       int64(50001),
						"quantity":     int64(683),
						"inventoryKey": int64(1),
						"instanceId":   int64(9002),
					},
				},
			},
		}, nil
	}
	return b.fixtureBroker.Call(ctx, namespace, operation, arguments)
}

func TestMemoryFailureFallsBackToExplicitSave(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	broker := &saveFallbackBroker{}
	runner, _ := New(broker)
	output, err := runner.Run(context.Background(), pkg, map[string]any{
		"save": map[string]any{"root": "crimson-desert-saves", "relative": "slot/save.save"},
	})
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
	attempts := result["attempts"].([]any)
	if len(attempts) != 2 ||
		attempts[0].(map[string]any)["status"] != "failed" ||
		attempts[1].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("attempts = %#v", attempts)
	}
	wantCalls := []string{"memory.modules", "memory.scan", "file.decode"}
	if !reflect.DeepEqual(broker.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", broker.calls, wantCalls)
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

func TestNonEligibleBrokerFailureDoesNotFallback(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	broker := &failingBroker{}
	runner, _ := New(broker)
	_, err = runner.Run(context.Background(), pkg, nil)
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

func TestMissingSaveInputIsScriptFailureNotSourceFailure(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := New(&ambiguousBroker{})
	_, err = runner.Run(context.Background(), pkg, nil)
	var runError *Error
	if !errors.As(err, &runError) {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if runError.Code != "SCRIPT_RUNTIME_FAILED" {
		t.Fatalf("code = %q", runError.Code)
	}
}
