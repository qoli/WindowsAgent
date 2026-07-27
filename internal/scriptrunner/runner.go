// Package scriptrunner executes one validated Starlark observation package.
package scriptrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type Broker interface {
	Call(ctx context.Context, namespace, operation string, arguments map[string]any) (any, error)
	BlobPath(ctx context.Context, reference map[string]any) (string, error)
	RecordNative(ctx context.Context, record NativeRecord) error
}

type NativeRecord struct {
	Alias             string `json:"alias"`
	Action            string `json:"action"`
	Function          string `json:"function,omitempty"`
	Phase             string `json:"phase"`
	CallsUsed         uint32 `json:"callsUsed"`
	NativeMemoryBytes uint64 `json:"nativeMemoryBytes"`
	ResultBytes       uint64 `json:"resultBytes"`
	ErrorKind         string `json:"errorKind,omitempty"`
}

// BrokerError preserves the observer error classification across the broker
// boundary. FallbackEligible must only be true for a failure of the selected
// observation source itself, never for protocol, permission, budget, deadline,
// or host-integration failures.
type BrokerError struct {
	Code             string
	FallbackEligible bool
	Cause            error
}

func (e *BrokerError) Error() string {
	if e.Cause == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Cause)
}

func (e *BrokerError) Unwrap() error {
	return e.Cause
}

type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Code
	}
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

type Runner struct {
	broker        Broker
	nativeBackend nativeBackend
}

func New(broker Broker) (*Runner, error) {
	if broker == nil {
		return nil, errors.New("broker is required")
	}
	return &Runner{broker: broker, nativeBackend: newNativeBackend()}, nil
}

func (r *Runner) Run(ctx context.Context, pkg *scriptpackage.Package, inputs map[string]any) ([]byte, error) {
	if pkg == nil {
		return nil, &Error{Code: "SCRIPT_PACKAGE_INVALID", Stage: "validating-package", Cause: errors.New("package is required")}
	}
	if ctx == nil {
		return nil, &Error{Code: "SCRIPT_RUNTIME_FAILED", Stage: "starting-script", Cause: errors.New("context is required")}
	}
	runContext, cancel := context.WithTimeout(ctx, time.Duration(pkg.Manifest.Limits.WallTimeMS)*time.Millisecond)
	defer cancel()

	host := &host{ctx: runContext, broker: r.broker, pkg: pkg, inputs: cloneInputs(inputs)}
	host.native = newNativeHost(runContext, pkg, r.broker, r.nativeBackend)
	defer host.native.close()
	predeclared, err := host.predeclared()
	if err != nil {
		return nil, &Error{Code: "SCRIPT_RUNTIME_FAILED", Stage: "building-api", Cause: err}
	}
	thread := &starlark.Thread{Name: pkg.Identity.ID}
	thread.SetMaxExecutionSteps(pkg.Manifest.Limits.MaxSteps)
	thread.Print = func(_ *starlark.Thread, _ string) {
		// Starlark's universe contains print, but the host deliberately discards
		// it so stdout remains a protocol-only channel.
	}
	var cancelOnce sync.Once
	done := make(chan struct{})
	go func() {
		select {
		case <-runContext.Done():
			cancelOnce.Do(func() { thread.Cancel(runContext.Err().Error()) })
		case <-done:
		}
	}()
	defer close(done)

	globals, err := starlark.ExecFile(thread, pkg.Manifest.Entrypoint, pkg.Script, predeclared)
	if err != nil {
		return nil, classifyRuntimeError(runContext, err)
	}
	entrypoint, ok := globals["main"]
	if !ok {
		return nil, &Error{Code: "SCRIPT_STATIC_INVALID", Stage: "resolving-main", Cause: errors.New("main(ctx) is required")}
	}
	callable, ok := entrypoint.(starlark.Callable)
	if !ok {
		return nil, &Error{Code: "SCRIPT_STATIC_INVALID", Stage: "resolving-main", Cause: errors.New("main must be callable")}
	}
	inputValue, err := toStarlark(host.inputs)
	if err != nil {
		return nil, &Error{Code: "SCRIPT_RUNTIME_FAILED", Stage: "building-context", Cause: err}
	}
	ctxValue := starlarkstruct.FromStringDict(starlark.String("job_context"), starlark.StringDict{
		"inputs": inputValue,
	})
	value, err := starlark.Call(thread, callable, starlark.Tuple{ctxValue}, nil)
	if err != nil {
		return nil, classifyRuntimeError(runContext, err)
	}
	native, err := fromStarlark(value)
	if err != nil {
		return nil, &Error{Code: "OUTPUT_NOT_JSON", Stage: "serializing-output", Cause: err}
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		return nil, &Error{Code: "OUTPUT_NOT_JSON", Stage: "serializing-output", Cause: err}
	}
	if uint64(len(encoded)) > pkg.Manifest.Limits.MaxResultBytes {
		return nil, &Error{
			Code:  "SCRIPT_LIMIT_EXCEEDED",
			Stage: "serializing-output",
			Cause: fmt.Errorf("result is %d bytes, limit is %d", len(encoded), pkg.Manifest.Limits.MaxResultBytes),
		}
	}
	if err := pkg.ValidateOutput(native); err != nil {
		return nil, &Error{Code: "OUTPUT_SCHEMA_INVALID", Stage: "validating-output", Cause: err}
	}
	return encoded, nil
}

func classifyRuntimeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		code := "JOB_CANCELLED"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "JOB_DEADLINE_EXCEEDED"
		}
		return &Error{Code: code, Stage: "executing-script", Cause: ctx.Err()}
	}
	if strings.Contains(err.Error(), "too many steps") {
		return &Error{Code: "SCRIPT_LIMIT_EXCEEDED", Stage: "executing-script", Cause: err}
	}
	var abort *abortError
	if errors.As(err, &abort) {
		return &Error{Code: abort.code, Stage: "executing-script", Cause: errors.New(abort.message)}
	}
	var brokerFailure *brokerCallError
	if errors.As(err, &brokerFailure) {
		code := brokerFailure.code
		if code == "" {
			code = "OBSERVER_BROKER_FAILED"
		}
		return &Error{Code: code, Stage: "brokering-observer-call", Cause: brokerFailure}
	}
	var nativeFailure *nativeError
	if errors.As(err, &nativeFailure) {
		return &Error{Code: nativeFailure.code, Stage: nativeFailure.stage, Cause: nativeFailure.cause}
	}
	return &Error{Code: "SCRIPT_RUNTIME_FAILED", Stage: "executing-script", Cause: err}
}

type abortError struct {
	code    string
	message string
}

type brokerCallError struct {
	namespace string
	operation string
	code      string
	eligible  bool
	cause     error
}

func (e *brokerCallError) Error() string {
	return fmt.Sprintf("observer %s.%s: %v", e.namespace, e.operation, e.cause)
}

func (e *brokerCallError) Unwrap() error {
	return e.cause
}

func (e *abortError) Error() string {
	return e.code + ": " + e.message
}

type host struct {
	ctx    context.Context
	broker Broker
	pkg    *scriptpackage.Package
	inputs map[string]any
	native *nativeHost
}

func (h *host) predeclared() (starlark.StringDict, error) {
	memory := starlark.StringDict{}
	if permission := h.pkg.Manifest.Permissions.Memory; permission != nil {
		for _, operation := range permission.Operations {
			name := starlarkOperationName(operation)
			memory[name] = starlark.NewBuiltin("observer.memory."+name, h.callBuiltin(observationapi.NamespaceMemory, operation))
		}
	}
	file := starlark.StringDict{}
	if permission := h.pkg.Manifest.Permissions.File; permission != nil {
		for _, operation := range permission.Operations {
			name := starlarkOperationName(operation)
			file[name] = starlark.NewBuiltin("observer.file."+name, h.callBuiltin(observationapi.NamespaceFile, operation))
		}
	}
	observerFields := starlark.StringDict{}
	if len(memory) != 0 {
		observerFields["memory"] = starlarkstruct.FromStringDict(starlark.String("memory"), memory)
	}
	if len(file) != 0 {
		observerFields["file"] = starlarkstruct.FromStringDict(starlark.String("file"), file)
	}
	job := starlarkstruct.FromStringDict(starlark.String("job"), starlark.StringDict{
		"input":   starlark.NewBuiltin("job.input", h.inputBuiltin),
		"attempt": starlark.NewBuiltin("job.attempt", h.attemptBuiltin),
		"fail":    starlark.NewBuiltin("job.fail", h.failBuiltin),
	})
	return starlark.StringDict{
		"observer": starlarkstruct.FromStringDict(starlark.String("observer"), observerFields),
		"native":   h.native.module(),
		"job":      job,
	}, nil
}

func (h *host) callBuiltin(namespace, operation string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 0 {
			return nil, errors.New("observer calls accept named arguments only")
		}
		arguments := make(map[string]any, len(kwargs))
		for _, item := range kwargs {
			name, ok := starlark.AsString(item[0])
			if !ok || name == "" {
				return nil, errors.New("observer argument name must be a string")
			}
			if _, exists := arguments[name]; exists {
				return nil, fmt.Errorf("duplicate observer argument %q", name)
			}
			value, err := fromStarlark(item[1])
			if err != nil {
				return nil, fmt.Errorf("observer argument %q: %w", name, err)
			}
			arguments[name] = value
		}
		result, err := h.broker.Call(h.ctx, namespace, operation, arguments)
		if err != nil {
			failure := &brokerCallError{
				namespace: namespace,
				operation: operation,
				code:      "OBSERVER_BROKER_FAILED",
				cause:     err,
			}
			var typed *BrokerError
			if errors.As(err, &typed) {
				failure.code = typed.Code
				failure.eligible = typed.FallbackEligible
			}
			return nil, failure
		}
		return toStarlark(result)
	}
}

func (h *host) inputBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("job.input", args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	value, ok := h.inputs[name]
	if !ok {
		return nil, fmt.Errorf("job input %q is not present", name)
	}
	return toStarlark(value)
}

func (h *host) failBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var code, message string
	if err := starlark.UnpackArgs("job.fail", args, kwargs, "code", &code, "message", &message); err != nil {
		return nil, err
	}
	if code == "" || message == "" {
		return nil, errors.New("job.fail code and message must not be empty")
	}
	return nil, &abortError{code: code, message: message}
}

func (h *host) attemptBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var source string
	var function starlark.Callable
	if err := starlark.UnpackArgs(
		"job.attempt",
		args,
		kwargs,
		"source",
		&source,
		"function",
		&function,
	); err != nil {
		return nil, err
	}
	if source == "" {
		return nil, errors.New("job.attempt source must not be empty")
	}
	value, err := starlark.Call(thread, function, nil, nil)
	if err == nil {
		native, convertErr := fromStarlark(value)
		if convertErr != nil {
			return nil, fmt.Errorf("job.attempt result is not JSON-compatible: %w", convertErr)
		}
		return toStarlark(map[string]any{
			"ok":     true,
			"source": source,
			"error":  nil,
			"value":  native,
		})
	}
	var abort *abortError
	if errors.As(err, &abort) {
		return toStarlark(map[string]any{
			"ok":     false,
			"source": source,
			"value":  nil,
			"error": map[string]any{
				"code":    abort.code,
				"message": abort.message,
			},
		})
	}
	var brokerFailure *brokerCallError
	if errors.As(err, &brokerFailure) {
		if !brokerFailure.eligible {
			return nil, err
		}
		return toStarlark(map[string]any{
			"ok":     false,
			"source": source,
			"value":  nil,
			"error": map[string]any{
				"code":    brokerFailure.code,
				"message": brokerFailure.Error(),
			},
		})
	}
	return nil, err
}

func starlarkOperationName(operation string) string {
	var result strings.Builder
	for index, char := range operation {
		if char >= 'A' && char <= 'Z' {
			if index != 0 {
				result.WriteByte('_')
			}
			result.WriteRune(char + ('a' - 'A'))
			continue
		}
		result.WriteRune(char)
	}
	return result.String()
}

func cloneInputs(inputs map[string]any) map[string]any {
	if inputs == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(inputs))
	for key, value := range inputs {
		result[key] = value
	}
	return result
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
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("invalid JSON number %q", value)
		}
		return starlark.Float(number), nil
	case int:
		return starlark.MakeInt(value), nil
	case int32:
		return starlark.MakeInt64(int64(value)), nil
	case int64:
		return starlark.MakeInt64(value), nil
	case uint:
		return starlark.MakeUint64(uint64(value)), nil
	case uint32:
		return starlark.MakeUint64(uint64(value)), nil
	case uint64:
		return starlark.MakeUint64(value), nil
	case float32:
		return finiteFloat(float64(value))
	case float64:
		return finiteFloat(value)
	case []any:
		items := make([]starlark.Value, len(value))
		for index, item := range value {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", index, err)
			}
			items[index] = converted
		}
		return starlark.NewList(items), nil
	case map[string]any:
		dict := starlark.NewDict(len(value))
		for key, item := range value {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, fmt.Errorf("object field %q: %w", key, err)
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("unsupported value %T", value)
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
		for _, item := range value.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, errors.New("JSON object keys must be strings")
			}
			converted, err := fromStarlark(item[1])
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
