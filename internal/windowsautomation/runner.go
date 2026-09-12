package windowsautomation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qoli/WindowsAgent/internal/eventstream"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

type Reporter interface {
	Emit(context.Context, string, json.RawMessage) (eventstream.Event, error)
}

type Runner struct {
	processes ProcessHost
	files     FileHost
	now       func() time.Time
}

func NewRunner(processes ProcessHost, files FileHost) (*Runner, error) {
	if processes == nil || files == nil {
		return nil, errors.New("process and filesystem hosts are required")
	}
	return &Runner{processes: processes, files: files, now: time.Now}, nil
}

func NewLocalRunner() (*Runner, error) {
	return NewRunner(OSProcessHost{}, OSFileHost{})
}

func (r *Runner) Run(ctx context.Context, pkg *Package, inputs map[string]any, reporter Reporter) (json.RawMessage, error) {
	if r == nil || r.processes == nil || r.files == nil {
		return nil, &Error{Code: "AUTOMATION_RUNTIME_INVALID", Stage: "starting-action", Cause: errors.New("runner is required")}
	}
	if ctx == nil || pkg == nil || reporter == nil {
		return nil, &Error{Code: "AUTOMATION_RUNTIME_INVALID", Stage: "starting-action", Cause: errors.New("context, package, and reporter are required")}
	}
	if inputs == nil {
		return nil, &Error{Code: "AUTOMATION_INPUT_INVALID", Stage: "validating-inputs", Cause: errors.New("inputs object is required")}
	}
	if err := pkg.ValidateInputs(inputs); err != nil {
		return nil, &Error{Code: "AUTOMATION_INPUT_INVALID", Stage: "validating-inputs", Cause: err}
	}
	runContext, cancel := context.WithTimeout(ctx, time.Duration(pkg.Manifest.Limits.WallTimeMS)*time.Millisecond)
	defer cancel()
	host := runtimeHost{ctx: runContext, pkg: pkg, processes: r.processes, files: r.files, reporter: reporter, now: r.now, started: r.now()}
	thread := &starlark.Thread{Name: pkg.Manifest.Title}
	thread.SetMaxExecutionSteps(pkg.Manifest.Limits.MaxSteps)
	thread.Print = func(*starlark.Thread, string) {}
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
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{While: true}, thread, pkg.Manifest.Entrypoint, pkg.Script, host.predeclared())
	if err != nil {
		return nil, classifyRuntimeError(runContext, err)
	}
	entrypoint, ok := globals["main"]
	if !ok {
		return nil, &Error{Code: "AUTOMATION_STATIC_INVALID", Stage: "resolving-main", Cause: errors.New("main(ctx) is required")}
	}
	callable, ok := entrypoint.(starlark.Callable)
	if !ok {
		return nil, &Error{Code: "AUTOMATION_STATIC_INVALID", Stage: "resolving-main", Cause: errors.New("main must be callable")}
	}
	inputValue, err := toStarlark(inputs)
	if err != nil {
		return nil, &Error{Code: "AUTOMATION_INPUT_INVALID", Stage: "building-context", Cause: err}
	}
	contextValue := starlarkstruct.FromStringDict(starlark.String("action_context"), starlark.StringDict{"inputs": inputValue})
	value, err := starlark.Call(thread, callable, starlark.Tuple{contextValue}, nil)
	if err != nil {
		return nil, classifyRuntimeError(runContext, err)
	}
	native, err := fromStarlark(value)
	if err != nil {
		return nil, &Error{Code: "AUTOMATION_OUTPUT_NOT_JSON", Stage: "serializing-output", Cause: err}
	}
	if err := pkg.ValidateOutput(native); err != nil {
		return nil, &Error{Code: "AUTOMATION_OUTPUT_INVALID", Stage: "validating-output", Cause: err}
	}
	encoded, err := json.Marshal(native)
	if err != nil {
		return nil, &Error{Code: "AUTOMATION_OUTPUT_NOT_JSON", Stage: "serializing-output", Cause: err}
	}
	if uint64(len(encoded)) > pkg.Manifest.Limits.MaxResultBytes {
		return nil, &Error{Code: "AUTOMATION_RESULT_LIMIT_EXCEEDED", Stage: "serializing-output", Cause: fmt.Errorf("result is %d bytes, limit is %d", len(encoded), pkg.Manifest.Limits.MaxResultBytes)}
	}
	return encoded, nil
}

type runtimeHost struct {
	ctx       context.Context
	pkg       *Package
	processes ProcessHost
	files     FileHost
	reporter  Reporter
	now       func() time.Time
	started   time.Time
}

func (h *runtimeHost) predeclared() starlark.StringDict {
	process := starlarkstruct.FromStringDict(starlark.String("windows.process"), starlark.StringDict{
		"run": starlark.NewBuiltin("windows.process.run", h.processRun),
	})
	filesystem := starlarkstruct.FromStringDict(starlark.String("windows.fs"), starlark.StringDict{
		"read": starlark.NewBuiltin("windows.fs.read", h.fileRead), "write": starlark.NewBuiltin("windows.fs.write", h.fileWrite),
		"stat": starlark.NewBuiltin("windows.fs.stat", h.fileStat), "list": starlark.NewBuiltin("windows.fs.list", h.fileList),
		"mkdir": starlark.NewBuiltin("windows.fs.mkdir", h.fileMkdir), "copy": starlark.NewBuiltin("windows.fs.copy", h.fileCopy),
		"move": starlark.NewBuiltin("windows.fs.move", h.fileMove), "remove": starlark.NewBuiltin("windows.fs.remove", h.fileRemove),
	})
	windowsModule := starlarkstruct.FromStringDict(starlark.String("windows"), starlark.StringDict{"process": process, "fs": filesystem})
	taskModule := starlarkstruct.FromStringDict(starlark.String("task"), starlark.StringDict{
		"activity":             starlark.NewBuiltin("task.activity", h.activity),
		"elapsed_milliseconds": starlark.NewBuiltin("task.elapsed_milliseconds", h.elapsedMilliseconds),
		"fail":                 starlark.NewBuiltin("task.fail", h.fail),
	})
	return starlark.StringDict{"windows": windowsModule, "task": taskModule}
}

func (h *runtimeHost) processRun(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("windows.process.run", args); err != nil {
		return nil, err
	}
	var executable, cwd, stdin string
	var argv *starlark.List
	var environment *starlark.Dict
	if err := starlark.UnpackArgs("windows.process.run", nil, kwargs, "executable", &executable, "argv", &argv, "cwd?", &cwd, "env?", &environment, "stdin?", &stdin); err != nil {
		return nil, err
	}
	if executable == "" || strings.TrimSpace(executable) != executable || argv == nil {
		return nil, errors.New("windows.process.run requires canonical executable and argv list")
	}
	arguments, err := stringList(argv, "argv")
	if err != nil {
		return nil, err
	}
	env, err := stringDict(environment, "env")
	if err != nil {
		return nil, err
	}
	result, err := h.processes.Run(h.ctx, ProcessRequest{
		Executable: executable, Argv: arguments, Cwd: cwd, Env: env, Stdin: stdin,
		MaxOutputBytes: h.pkg.Manifest.Limits.MaxProcessOutputBytes,
	})
	if err != nil {
		return nil, hostError(h.ctx, "PROCESS_RUN_FAILED", "windows.process.run", err)
	}
	return toStarlark(map[string]any{
		"exitCode": result.ExitCode, "stdout": result.Stdout, "stderr": result.Stderr,
		"pid": result.PID, "durationMs": result.DurationMS,
	})
}

func (h *runtimeHost) fileRead(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name, err := onePath("windows.fs.read", args, kwargs)
	if err != nil {
		return nil, err
	}
	data, err := h.files.ReadFile(h.ctx, name)
	if err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.read", err)
	}
	if !utf8.Valid(data) {
		return nil, &Error{Code: "FILE_CONTENT_NOT_UTF8", Stage: "windows.fs.read", Cause: errors.New("file content is not UTF-8")}
	}
	return starlark.String(string(data)), nil
}

func (h *runtimeHost) fileWrite(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("windows.fs.write", args); err != nil {
		return nil, err
	}
	var name, content string
	if err := starlark.UnpackArgs("windows.fs.write", nil, kwargs, "path", &name, "content", &content); err != nil {
		return nil, err
	}
	if err := requiredPath(name); err != nil {
		return nil, err
	}
	if err := h.files.WriteFile(h.ctx, name, []byte(content)); err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.write", err)
	}
	return starlark.None, nil
}

func (h *runtimeHost) fileStat(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name, err := onePath("windows.fs.stat", args, kwargs)
	if err != nil {
		return nil, err
	}
	info, err := h.files.Stat(h.ctx, name)
	if err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.stat", err)
	}
	return toStarlark(infoMap(info))
}

func (h *runtimeHost) fileList(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name, err := onePath("windows.fs.list", args, kwargs)
	if err != nil {
		return nil, err
	}
	entries, err := h.files.List(h.ctx, name)
	if err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.list", err)
	}
	values := make([]any, len(entries))
	for index, entry := range entries {
		values[index] = infoMap(entry)
	}
	return toStarlark(values)
}

func (h *runtimeHost) fileMkdir(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("windows.fs.mkdir", args); err != nil {
		return nil, err
	}
	var name string
	var parents bool
	if err := starlark.UnpackArgs("windows.fs.mkdir", nil, kwargs, "path", &name, "parents?", &parents); err != nil {
		return nil, err
	}
	if err := requiredPath(name); err != nil {
		return nil, err
	}
	if err := h.files.Mkdir(h.ctx, name, parents); err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.mkdir", err)
	}
	return starlark.None, nil
}

func (h *runtimeHost) fileCopy(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return h.twoPathOperation("windows.fs.copy", args, kwargs, h.files.Copy)
}

func (h *runtimeHost) fileMove(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return h.twoPathOperation("windows.fs.move", args, kwargs, h.files.Move)
}

func (h *runtimeHost) twoPathOperation(name string, args starlark.Tuple, kwargs []starlark.Tuple, operation func(context.Context, string, string, bool) error) (starlark.Value, error) {
	if err := namedOnly(name, args); err != nil {
		return nil, err
	}
	var source, destination string
	var overwrite bool
	if err := starlark.UnpackArgs(name, nil, kwargs, "source", &source, "destination", &destination, "overwrite?", &overwrite); err != nil {
		return nil, err
	}
	if err := requiredPath(source); err != nil {
		return nil, err
	}
	if err := requiredPath(destination); err != nil {
		return nil, err
	}
	if err := operation(h.ctx, source, destination, overwrite); err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", name, err)
	}
	return starlark.None, nil
}

func (h *runtimeHost) fileRemove(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("windows.fs.remove", args); err != nil {
		return nil, err
	}
	var name string
	var recursive bool
	if err := starlark.UnpackArgs("windows.fs.remove", nil, kwargs, "path", &name, "recursive?", &recursive); err != nil {
		return nil, err
	}
	if err := requiredPath(name); err != nil {
		return nil, err
	}
	if err := h.files.Remove(h.ctx, name, recursive); err != nil {
		return nil, hostError(h.ctx, "FILESYSTEM_OPERATION_FAILED", "windows.fs.remove", err)
	}
	return starlark.None, nil
}

func (h *runtimeHost) activity(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("task.activity", args); err != nil {
		return nil, err
	}
	var message, level string
	if err := starlark.UnpackArgs("task.activity", nil, kwargs, "message", &message, "level", &level); err != nil {
		return nil, err
	}
	if message == "" || strings.TrimSpace(message) != message || strings.ContainsAny(message, "\r\n\t") || !utf8.ValidString(message) {
		return nil, errors.New("task.activity message must be one canonical non-empty line")
	}
	if level != "info" && level != "warning" && level != "error" {
		return nil, errors.New("task.activity level must equal info, warning, or error")
	}
	payload, err := json.Marshal(map[string]string{"message": message, "level": level})
	if err != nil {
		return nil, err
	}
	event, err := h.reporter.Emit(h.ctx, ActivityEventType, payload)
	if err != nil {
		return nil, hostError(h.ctx, "ACTIVITY_COMMIT_FAILED", "task.activity", err)
	}
	return starlark.MakeUint64(event.Sequence), nil
}

func (h *runtimeHost) elapsedMilliseconds(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 || len(kwargs) != 0 {
		return nil, errors.New("task.elapsed_milliseconds accepts no arguments")
	}
	elapsed := h.now().Sub(h.started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return starlark.MakeInt64(elapsed), nil
}

func (h *runtimeHost) fail(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := namedOnly("task.fail", args); err != nil {
		return nil, err
	}
	var code, message string
	if err := starlark.UnpackArgs("task.fail", nil, kwargs, "code", &code, "message", &message); err != nil {
		return nil, err
	}
	if !validFailureCode(code) {
		return nil, errors.New("task.fail code must contain only uppercase ASCII letters, digits, and underscores")
	}
	if message == "" || strings.TrimSpace(message) != message || strings.ContainsAny(message, "\r\n\t") || !utf8.ValidString(message) {
		return nil, errors.New("task.fail message must be one canonical non-empty line")
	}
	return nil, &Error{Code: code, Stage: "task.fail", Cause: errors.New(message)}
}

func validFailureCode(code string) bool {
	if code == "" {
		return false
	}
	for _, char := range code {
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func onePath(name string, args starlark.Tuple, kwargs []starlark.Tuple) (string, error) {
	if err := namedOnly(name, args); err != nil {
		return "", err
	}
	var path string
	if err := starlark.UnpackArgs(name, nil, kwargs, "path", &path); err != nil {
		return "", err
	}
	if err := requiredPath(path); err != nil {
		return "", err
	}
	return path, nil
}

func namedOnly(name string, args starlark.Tuple) error {
	if len(args) != 0 {
		return fmt.Errorf("%s accepts named arguments only", name)
	}
	return nil
}

func requiredPath(name string) error {
	if name == "" {
		return errors.New("filesystem path is required")
	}
	return nil
}

func stringList(list *starlark.List, field string) ([]string, error) {
	result := make([]string, 0, list.Len())
	iterator := list.Iterate()
	defer iterator.Done()
	var value starlark.Value
	for iterator.Next(&value) {
		item, ok := starlark.AsString(value)
		if !ok {
			return nil, fmt.Errorf("%s must contain only strings", field)
		}
		result = append(result, item)
	}
	return result, nil
}

func stringDict(dict *starlark.Dict, field string) (map[string]string, error) {
	if dict == nil {
		return nil, nil
	}
	result := make(map[string]string, dict.Len())
	for _, item := range dict.Items() {
		key, keyOK := starlark.AsString(item[0])
		value, valueOK := starlark.AsString(item[1])
		if !keyOK || !valueOK || key == "" || strings.Contains(key, "=") {
			return nil, fmt.Errorf("%s must map canonical environment names to strings", field)
		}
		result[key] = value
	}
	return result, nil
}

func infoMap(info FileInfo) map[string]any {
	return map[string]any{"name": info.Name, "path": info.Path, "size": info.Size, "isDir": info.IsDir, "modifiedAt": info.ModifiedAt}
}

func hostError(ctx context.Context, code, stage string, err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	if ctx != nil && ctx.Err() != nil {
		return &Error{Code: cancellationCode(ctx.Err()), Stage: stage, Cause: ctx.Err()}
	}
	return &Error{Code: code, Stage: stage, Cause: err}
}

func classifyRuntimeError(ctx context.Context, err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	if ctx.Err() != nil {
		return &Error{Code: cancellationCode(ctx.Err()), Stage: "executing-action", Cause: ctx.Err()}
	}
	if strings.Contains(err.Error(), "too many steps") {
		return &Error{Code: "AUTOMATION_STEP_LIMIT_EXCEEDED", Stage: "executing-action", Cause: err}
	}
	return &Error{Code: "AUTOMATION_RUNTIME_FAILED", Stage: "executing-action", Cause: err}
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
		floating, err := value.Float64()
		if err != nil {
			return nil, err
		}
		return starlark.Float(floating), nil
	case int:
		return starlark.MakeInt(value), nil
	case int64:
		return starlark.MakeInt64(value), nil
	case uint64:
		return starlark.MakeUint64(value), nil
	case float64:
		return starlark.Float(value), nil
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
			return nil, fmt.Errorf("convert %T to JSON: %w", value, err)
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

func fromStarlark(value starlark.Value) (any, error) {
	switch value := value.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(value), nil
	case starlark.String:
		return string(value), nil
	case starlark.Int:
		if integer, ok := value.Int64(); ok {
			return integer, nil
		}
		return nil, errors.New("Starlark integer is outside signed 64-bit JSON range")
	case starlark.Float:
		floating := float64(value)
		if math.IsInf(floating, 0) || math.IsNaN(floating) {
			return nil, errors.New("non-finite float is not JSON")
		}
		return floating, nil
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
				return nil, errors.New("Starlark JSON object keys must be strings")
			}
			converted, err := fromStarlark(item[1])
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("Starlark value %s is not JSON-compatible", value.Type())
	}
}
