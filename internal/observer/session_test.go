package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

type countingBackend struct{}

func (countingBackend) Call(context.Context, string, string, map[string]any) (BackendResult, error) {
	return BackendResult{Value: map[string]any{"ok": true}, MemoryBytesRead: 4}, nil
}

type screenCountingBackend struct{}

func (screenCountingBackend) Call(context.Context, string, string, map[string]any) (BackendResult, error) {
	return BackendResult{Value: map[string]any{"ok": true}, ScreenPixelsRead: 4}, nil
}

func TestSessionEnforcesNamespaceCallLimit(t *testing.T) {
	session, err := NewSession("job-1", scriptpackage.Permissions{
		Memory: &scriptpackage.MemoryPermissions{
			Target:       "fixture",
			Operations:   []string{"readBatch"},
			MaxCalls:     1,
			MaxBytesRead: 8,
		},
	}, countingBackend{})
	if err != nil {
		t.Fatal(err)
	}
	call := observationapi.Call{
		JobID:          "job-1",
		ObserverCallID: "call-1",
		Namespace:      "memory",
		Operation:      "readBatch",
		Arguments:      json.RawMessage(`{"reads":[]}`),
	}
	if _, err := session.Call(context.Background(), call); err != nil {
		t.Fatalf("first call: %v", err)
	}
	call.ObserverCallID = "call-2"
	_, err = session.Call(context.Background(), call)
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "LIMIT_EXCEEDED" {
		t.Fatalf("second call error = %#v", err)
	}
}

func TestSessionEnforcesScreenCallAndPixelLimits(t *testing.T) {
	session, err := NewSession("job-screen", scriptpackage.Permissions{
		Screen: &scriptpackage.ScreenPermissions{
			Operations: []string{"readRegion"}, MaxCalls: 1, MaxPixels: 4,
		},
	}, screenCountingBackend{})
	if err != nil {
		t.Fatal(err)
	}
	call := observationapi.Call{
		JobID: "job-screen", ObserverCallID: "screen-1",
		Namespace: "screen", Operation: "readRegion", Arguments: json.RawMessage(`{}`),
	}
	result, err := session.Call(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if result.Accounting.ScreenPixelsRead != 4 {
		t.Fatalf("screen pixel accounting = %d", result.Accounting.ScreenPixelsRead)
	}
	call.ObserverCallID = "screen-2"
	_, err = session.Call(context.Background(), call)
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "LIMIT_EXCEEDED" {
		t.Fatalf("second screen call error = %#v", err)
	}
}

func TestSessionAllowsDeclaredMultipleScreenCallsAndEnforcesCumulativePixels(t *testing.T) {
	session, err := NewSession("job-screen-multiple", scriptpackage.Permissions{
		Screen: &scriptpackage.ScreenPermissions{
			Operations: []string{"readRegion"}, MaxCalls: 4, MaxPixels: 12,
		},
	}, screenCountingBackend{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		call := observationapi.Call{
			JobID: "job-screen-multiple", ObserverCallID: fmt.Sprintf("screen-%d", index),
			Namespace: observationapi.NamespaceScreen, Operation: "readRegion", Arguments: json.RawMessage(`{}`),
		}
		result, err := session.Call(context.Background(), call)
		if err != nil {
			t.Fatalf("screen call %d: %v", index, err)
		}
		if result.Accounting.ScreenPixelsRead != uint64(index*4) {
			t.Fatalf("screen call %d cumulative pixels = %d", index, result.Accounting.ScreenPixelsRead)
		}
	}
	call := observationapi.Call{
		JobID: "job-screen-multiple", ObserverCallID: "screen-4",
		Namespace: observationapi.NamespaceScreen, Operation: "readRegion", Arguments: json.RawMessage(`{}`),
	}
	_, err = session.Call(context.Background(), call)
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "LIMIT_EXCEEDED" {
		t.Fatalf("fourth screen call error = %#v", err)
	}
}
