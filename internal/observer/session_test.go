package observer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
)

type countingBackend struct{}

func (countingBackend) Call(context.Context, string, string, map[string]any) (BackendResult, error) {
	return BackendResult{Value: map[string]any{"ok": true}, MemoryBytesRead: 4}, nil
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
