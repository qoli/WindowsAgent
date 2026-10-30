package actionrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/rules"
)

const DirectPointerInputActionID = "windows/direct-pointer-input"

// InvokeDirectPointer uses the same pointer controller as Rule Actions, but
// does not resolve a Rule, a binding, or an installed Action package.
func (m *Manager) InvokeDirectPointer(ctx context.Context, request pointeraction.DirectClickRequest) (Invocation, error) {
	if m == nil || ctx == nil {
		return Invocation{}, errors.New("Action invocation manager and context are required")
	}
	if err := request.Validate(); err != nil {
		return Invocation{}, fmt.Errorf("validate direct pointer request: %w", err)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Invocation{}, err
	}
	var cloned pointeraction.DirectClickRequest
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return Invocation{}, err
	}
	identity, err := newInvocationID(m.random)
	if err != nil {
		return Invocation{}, err
	}
	observed, foregroundErr := m.foreground()
	if foregroundErr != nil {
		m.logger.Warn("direct_pointer_input_foreground_unavailable", "error", foregroundErr)
	}
	action := rules.Action{ID: DirectPointerInputActionID, Runtime: rules.WindowsPointerActionRuntimeV1,
		Execution: rules.ActionExecution{Completion: rules.CompletionStream, Lifecycle: rules.LifecycleLinear, Interruptible: true}}
	runContext, cancel := context.WithCancel(context.Background())
	instance := &run{manager: m, action: action, identity: identity, foreground: observed, foregroundUnavailable: foregroundErr != nil,
		ctx: runContext, cancel: cancel, state: StateRunning, directPointerRequest: &cloned}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return Invocation{}, errors.New("Action invocation manager is closed")
	}
	m.runs[identity] = instance
	m.mu.Unlock()
	digest := sha256.Sum256(encoded)
	started, err := instance.appendEvent(context.Background(), "action.started", map[string]any{
		"state": StateRunning, "actionId": action.ID, "lifecycle": action.Execution.Lifecycle, "interruptible": true,
		"operation": request.Operation, "x": request.X, "y": request.Y, "holdMs": request.HoldMS,
		"expectedForeground": request.ExpectedForeground, "requestDigest": hex.EncodeToString(digest[:]),
	})
	if err != nil {
		cancel()
		m.mu.Lock()
		delete(m.runs, identity)
		m.mu.Unlock()
		return Invocation{}, fmt.Errorf("commit direct pointer start event: %w", err)
	}
	instance.mu.Lock()
	instance.afterCursor = started.Sequence - 1
	instance.mu.Unlock()
	m.wg.Add(1)
	go instance.execute()
	return Invocation{InvocationID: identity, ActionID: action.ID, Runtime: action.Runtime, State: StateRunning, Execution: action.Execution,
		Watch: &WatchTarget{URL: "/v1/action-invocations/" + identity + "/events?after=" + fmt.Sprint(started.Sequence-1), ContentType: "application/x-ndjson", AfterCursor: started.Sequence - 1},
		Stop:  &StopTarget{Method: "POST", URL: "/v1/action-invocations/" + identity + "/stop"}}, nil
}
