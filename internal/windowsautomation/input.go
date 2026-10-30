package windowsautomation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"go.starlark.net/starlark"
)

type KeyboardHost interface {
	PressDirect(context.Context, inputaction.DirectPressRequest) (inputaction.DirectPressResult, error)
}

type PointerHost interface {
	ClickDirect(context.Context, pointeraction.DirectClickRequest) (pointeraction.DirectClickResult, error)
}

func expectedIdentity(value *starlark.Dict) (foreground.Identity, error) {
	if value == nil {
		return foreground.Identity{}, errors.New("expected_foreground is required")
	}
	native, err := fromStarlark(value)
	if err != nil {
		return foreground.Identity{}, err
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		return foreground.Identity{}, err
	}
	var identity foreground.Identity
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return identity, err
	}
	return identity, identity.Validate()
}

func resultValue(result any) (starlark.Value, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var native any
	if err := decoder.Decode(&native); err != nil {
		return nil, err
	}
	return toStarlark(native)
}

func (h *runtimeHost) keyPress(_ *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly(builtin.Name(), args); err != nil {
		return nil, err
	}
	var expected *starlark.Dict
	var key string
	var hold uint32
	if err := starlark.UnpackArgs(builtin.Name(), nil, kwargs, "expected_foreground", &expected, "key", &key, "hold_ms", &hold); err != nil {
		return nil, err
	}
	identity, err := expectedIdentity(expected)
	if err != nil {
		return nil, err
	}
	request := inputaction.DirectPressRequest{SchemaVersion: 1, ExpectedForeground: identity, Key: key, HoldMS: hold}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if h.keyboard == nil {
		return nil, &Error{Code: "AUTOMATION_INPUT_UNAVAILABLE", Stage: builtin.Name(), Cause: errors.New("keyboard host is not configured")}
	}
	result, err := h.keyboard.PressDirect(h.ctx, request)
	if err != nil {
		return nil, err
	}
	return resultValue(result)
}

func (h *runtimeHost) pointerClick(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return h.click(builtin, args, kwargs, true)
}

func (h *runtimeHost) pointerClickCurrent(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return h.click(builtin, args, kwargs, false)
}

func (h *runtimeHost) click(builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple, coordinates bool) (starlark.Value, error) {
	if err := namedOnly(builtin.Name(), args); err != nil {
		return nil, err
	}
	var expected *starlark.Dict
	var hold uint32
	var x, y int
	fields := []any{"expected_foreground", &expected, "hold_ms", &hold}
	if coordinates {
		fields = append(fields, "x", &x, "y", &y)
	}
	if err := starlark.UnpackArgs(builtin.Name(), nil, kwargs, fields...); err != nil {
		return nil, err
	}
	identity, err := expectedIdentity(expected)
	if err != nil {
		return nil, err
	}
	request := pointeraction.DirectClickRequest{SchemaVersion: 1, ExpectedForeground: identity, Operation: pointeraction.OperationClickCurrentPosition, HoldMS: hold}
	if coordinates {
		request.Operation, request.X, request.Y = pointeraction.OperationClickScreen, &x, &y
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if h.pointer == nil {
		return nil, &Error{Code: "AUTOMATION_INPUT_UNAVAILABLE", Stage: builtin.Name(), Cause: errors.New("pointer host is not configured")}
	}
	result, err := h.pointer.ClickDirect(h.ctx, request)
	if err != nil {
		return nil, err
	}
	return resultValue(result)
}

func (h *runtimeHost) sleep(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var milliseconds uint64
	if err := starlark.UnpackArgs("task.sleep", args, kwargs, "milliseconds", &milliseconds); err != nil {
		return nil, err
	}
	if milliseconds > h.pkg.Manifest.Limits.WallTimeMS {
		return nil, errors.New("task.sleep exceeds the package wallTimeMs")
	}
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-h.ctx.Done():
		return nil, h.ctx.Err()
	case <-timer.C:
		return starlark.None, nil
	}
}
