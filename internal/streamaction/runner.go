package streamaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type Caller interface {
	Call(context.Context, string, map[string]any) (json.RawMessage, error)
}

type Reporter interface {
	Emit(context.Context, string, json.RawMessage) (eventstream.Event, error)
}

type Runner struct {
	Sleep func(context.Context, time.Duration) error
}

func (r Runner) Run(ctx context.Context, pkg *Package, inputs map[string]any, caller Caller, reporter Reporter) (json.RawMessage, error) {
	if ctx == nil || pkg == nil || caller == nil || reporter == nil {
		return nil, errors.New("context, streaming Action package, child caller, and reporter are required")
	}
	if inputs == nil {
		return nil, errors.New("streaming Action inputs object is required")
	}
	if err := pkg.ValidateInput(inputs); err != nil {
		return nil, fmt.Errorf("streaming Action input schema: %w", err)
	}
	sleep := r.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	host := &host{ctx: ctx, pkg: pkg, caller: caller, reporter: reporter, sleepFn: sleep}
	thread := &starlark.Thread{Name: pkg.Manifest.Title}
	thread.SetMaxExecutionSteps(pkg.Manifest.Limits.MaxSteps)
	thread.Print = func(*starlark.Thread, string) {}
	var cancelOnce sync.Once
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancelOnce.Do(func() { thread.Cancel(ctx.Err().Error()) })
		case <-done:
		}
	}()
	defer close(done)
	globals, err := starlark.ExecFile(thread, pkg.Manifest.Entrypoint, pkg.Script, host.predeclared())
	if err != nil {
		return nil, runtimeError(ctx, err)
	}
	entrypoint, exists := globals["main"]
	if !exists {
		return nil, errors.New("streaming Action main(ctx) is required")
	}
	callable, ok := entrypoint.(starlark.Callable)
	if !ok {
		return nil, errors.New("streaming Action main must be callable")
	}
	inputValue, err := toStarlark(inputs)
	if err != nil {
		return nil, fmt.Errorf("convert streaming Action inputs: %w", err)
	}
	contextValue := starlarkstruct.FromStringDict(starlark.String("action_context"), starlark.StringDict{"inputs": inputValue})
	value, err := starlark.Call(thread, callable, starlark.Tuple{contextValue}, nil)
	if err != nil {
		return nil, runtimeError(ctx, err)
	}
	output, err := fromStarlark(value)
	if err != nil {
		return nil, fmt.Errorf("convert streaming Action output: %w", err)
	}
	if err := pkg.ValidateOutput(output); err != nil {
		return nil, fmt.Errorf("streaming Action output schema: %w", err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	if uint64(len(encoded)) > pkg.Manifest.Limits.MaxOutputBytes {
		return nil, fmt.Errorf("streaming Action output is %d bytes, limit is %d", len(encoded), pkg.Manifest.Limits.MaxOutputBytes)
	}
	return encoded, nil
}

type host struct {
	ctx      context.Context
	pkg      *Package
	caller   Caller
	reporter Reporter
	sleepFn  func(context.Context, time.Duration) error
}

func (h *host) predeclared() starlark.StringDict {
	actionModule := starlarkstruct.FromStringDict(starlark.String("action"), starlark.StringDict{
		"call": starlark.NewBuiltin("action.call", h.call),
	})
	streamModule := starlarkstruct.FromStringDict(starlark.String("stream"), starlark.StringDict{
		"emit": starlark.NewBuiltin("stream.emit", h.emit),
	})
	taskModule := starlarkstruct.FromStringDict(starlark.String("task"), starlark.StringDict{
		"sleep": starlark.NewBuiltin("task.sleep", h.sleep),
	})
	return starlark.StringDict{"action": actionModule, "stream": streamModule, "task": taskModule}
}

func (h *host) call(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var actionID string
	var inputs *starlark.Dict
	if err := starlark.UnpackArgs("action.call", args, kwargs, "id", &actionID, "inputs", &inputs); err != nil {
		return nil, err
	}
	if actionID == "" || strings.TrimSpace(actionID) != actionID || inputs == nil {
		return nil, errors.New("action.call requires canonical id and inputs object")
	}
	native, err := fromStarlark(inputs)
	if err != nil {
		return nil, err
	}
	inputMap, ok := native.(map[string]any)
	if !ok {
		return nil, errors.New("action.call inputs must be an object")
	}
	output, err := h.caller.Call(h.ctx, actionID, inputMap)
	if err != nil {
		return nil, fmt.Errorf("child Action %s failed: %w", actionID, err)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode child Action %s output: %w", actionID, err)
	}
	return toStarlark(decoded)
}

func (h *host) emit(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var eventType string
	var payload starlark.Value
	if err := starlark.UnpackArgs("stream.emit", args, kwargs, "type", &eventType, "payload", &payload); err != nil {
		return nil, err
	}
	native, err := fromStarlark(payload)
	if err != nil {
		return nil, err
	}
	envelope := map[string]any{"type": eventType, "payload": native}
	if err := h.pkg.ValidateEvent(envelope); err != nil {
		return nil, fmt.Errorf("streaming Action event schema: %w", err)
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		return nil, err
	}
	if uint64(len(encoded)) > h.pkg.Manifest.Limits.MaxEventBytes {
		return nil, fmt.Errorf("streaming Action event payload is %d bytes, limit is %d", len(encoded), h.pkg.Manifest.Limits.MaxEventBytes)
	}
	event, err := h.reporter.Emit(h.ctx, eventType, encoded)
	if err != nil {
		return nil, fmt.Errorf("commit streaming Action event: %w", err)
	}
	return starlark.MakeUint64(event.Sequence), nil
}

func (h *host) sleep(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var milliseconds int
	if err := starlark.UnpackArgs("task.sleep", args, kwargs, "milliseconds", &milliseconds); err != nil {
		return nil, err
	}
	if milliseconds < 1 || uint64(milliseconds) > h.pkg.Manifest.Limits.MaxSleepMs {
		return nil, fmt.Errorf("task.sleep milliseconds must be from 1 through %d", h.pkg.Manifest.Limits.MaxSleepMs)
	}
	if err := h.sleepFn(h.ctx, time.Duration(milliseconds)*time.Millisecond); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runtimeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if strings.Contains(err.Error(), "too many steps") {
		return fmt.Errorf("streaming Action step limit exceeded: %w", err)
	}
	return err
}

func toStarlark(value any) (starlark.Value, error) {
	switch value := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(value), nil
	case string:
		return starlark.String(value), nil
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			return starlark.MakeInt64(integer), nil
		}
		number, err := value.Float64()
		if err != nil {
			return nil, err
		}
		return finiteFloat(number)
	case int:
		return starlark.MakeInt(value), nil
	case int64:
		return starlark.MakeInt64(value), nil
	case uint64:
		return starlark.MakeUint64(value), nil
	case float64:
		return finiteFloat(value)
	case []any:
		items := make([]starlark.Value, len(value))
		for index, item := range value {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			items[index] = converted
		}
		return starlark.NewList(items), nil
	case map[string]any:
		dict := starlark.NewDict(len(value))
		for key, item := range value {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("unsupported JSON value %T", value)
		}
		var normalized any
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.UseNumber()
		if err := decoder.Decode(&normalized); err != nil {
			return nil, err
		}
		return toStarlark(normalized)
	}
}

func finiteFloat(value float64) (starlark.Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, errors.New("non-finite number is not JSON-compatible")
	}
	return starlark.Float(value), nil
}

func fromStarlark(value starlark.Value) (any, error) {
	switch value := value.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(value), nil
	case starlark.String:
		return string(value), nil
	case starlark.Int:
		integer, ok := value.Int64()
		if !ok {
			return nil, errors.New("integer is outside signed 64-bit JSON range")
		}
		return integer, nil
	case starlark.Float:
		number := float64(value)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("non-finite number is not JSON-compatible")
		}
		return number, nil
	case *starlark.List:
		result := make([]any, 0, value.Len())
		iterator := value.Iterate()
		defer iterator.Done()
		var item starlark.Value
		for iterator.Next(&item) {
			converted, err := fromStarlark(item)
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
		}
		return result, nil
	case starlark.Tuple:
		result := make([]any, len(value))
		for index, item := range value {
			converted, err := fromStarlark(item)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	case *starlark.Dict:
		result := make(map[string]any, value.Len())
		for _, pair := range value.Items() {
			key, ok := starlark.AsString(pair[0])
			if !ok {
				return nil, errors.New("JSON object keys must be strings")
			}
			converted, err := fromStarlark(pair[1])
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("Starlark %s is not JSON-compatible", value.Type())
	}
}
