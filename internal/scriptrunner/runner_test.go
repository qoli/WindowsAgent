package scriptrunner

import (
	"context"
	"encoding/binary"
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

func (b *fixtureBroker) BlobPath(context.Context, map[string]any) (string, error) {
	return "", errors.New("unexpected fixture blob path request")
}

func (b *fixtureBroker) RecordNative(_ context.Context, record NativeRecord) error {
	b.calls = append(b.calls, "native."+record.Action+"."+record.Phase)
	return nil
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
	if operation == "openBlob" {
		b.calls = append(b.calls, namespace+"."+operation)
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

type fixtureNativeBackend struct{}
type fixtureNativeDLL struct{}
type fixtureNativeProcedure struct{ name string }

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

func TestMemoryFailureFallsBackToExplicitSave(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "ObservationScripts", "CrimsonDesert", "inventory"))
	pkg, err := scriptpackage.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	broker := &saveFallbackBroker{}
	runner, _ := New(broker)
	runner.nativeBackend = fixtureNativeBackend{}
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
	if len(broker.calls) < 4 ||
		!reflect.DeepEqual(broker.calls[:4], []string{
			"memory.modules", "memory.scan", "file.openBlob", "native.blob_path",
		}) {
		t.Fatalf("call prefix = %#v", broker.calls)
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
