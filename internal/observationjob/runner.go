// Package observationjob launches and brokers one registered Script Package
// against one isolated unified observer process.
package observationjob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qoli/WindowsAgent/internal/observationapi"
	"github.com/qoli/WindowsAgent/internal/observationlauncher"
	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"github.com/qoli/WindowsAgent/internal/observer"
	"github.com/qoli/WindowsAgent/internal/scriptpackage"
	"github.com/qoli/WindowsAgent/internal/scriptrunner"
)

const maxScriptSnapshotMemberBytes = 4 << 20

type Spec struct {
	JobID                  string
	Deadline               time.Time
	CapabilityID           string
	PackageRoot            string
	ScriptRunnerExecutable string
	ObserverExecutable     string
	Process                *observer.ProcessIdentity
	FileRoots              map[string]string
	Inputs                 map[string]any
}

type Provenance struct {
	ObserverCallID string                     `json:"observerCallId,omitempty"`
	Namespace      string                     `json:"namespace"`
	Operation      string                     `json:"operation"`
	ObservedAt     *time.Time                 `json:"observedAt,omitempty"`
	Accounting     *observationapi.Accounting `json:"accounting,omitempty"`
	Native         *scriptrunner.NativeRecord `json:"native,omitempty"`
	ErrorKind      string                     `json:"errorKind,omitempty"`
}

type Result struct {
	Package    scriptpackage.Identity `json:"package"`
	Output     json.RawMessage        `json:"output"`
	Provenance []Provenance           `json:"provenance"`
}

type Error struct {
	Code  string
	Stage string
	Cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func Run(ctx context.Context, spec Spec) (_ Result, runErr error) {
	if ctx == nil {
		return Result{}, &Error{Code: "JOB_INVALID", Stage: "validating-spec", Cause: errors.New("context is required")}
	}
	if spec.JobID == "" || spec.Deadline.IsZero() || !spec.Deadline.After(time.Now()) {
		return Result{}, &Error{Code: "JOB_INVALID", Stage: "validating-spec", Cause: errors.New("jobId and future deadline are required")}
	}
	if spec.CapabilityID == "" {
		return Result{}, &Error{Code: "JOB_INVALID", Stage: "validating-spec", Cause: errors.New("capability ID is required")}
	}
	snapshotRoot, err := snapshotPackage(spec.PackageRoot)
	if err != nil {
		return Result{}, &Error{Code: "SCRIPT_PACKAGE_INVALID", Stage: "snapshotting-package", Cause: err}
	}
	defer os.RemoveAll(filepath.Dir(snapshotRoot))
	spec.PackageRoot = snapshotRoot
	pkg, err := scriptpackage.Load(spec.PackageRoot, spec.CapabilityID)
	if err != nil {
		return Result{}, &Error{Code: "SCRIPT_PACKAGE_INVALID", Stage: "loading-package", Cause: err}
	}
	if err := validateBindings(pkg, spec); err != nil {
		return Result{}, &Error{Code: "JOB_INVALID", Stage: "validating-bindings", Cause: err}
	}
	runContext, cancel := context.WithDeadline(ctx, spec.Deadline)
	defer cancel()
	blobRoot, err := os.MkdirTemp("", "windowsagent-observation-blob-")
	if err != nil {
		return Result{}, &Error{Code: "JOB_BLOB_FAILED", Stage: "creating-blob-root", Cause: err}
	}
	defer os.RemoveAll(blobRoot)
	blobs := newBlobCatalog(blobRoot)

	group, err := observationlauncher.NewGroup(observationlauncher.Limits{
		ActiveProcesses:    2,
		ProcessMemoryBytes: 768 << 20,
		JobMemoryBytes:     2 << 30,
	})
	if err != nil {
		return Result{}, &Error{Code: "LAUNCH_FAILED", Stage: "creating-job-object", Cause: err}
	}
	defer group.Close()
	go func() {
		<-runContext.Done()
		group.Close()
	}()

	scriptChild, err := group.Start(
		spec.ScriptRunnerExecutable,
		"--package-root", spec.PackageRoot,
		"--capability-id", spec.CapabilityID,
	)
	if err != nil {
		return Result{}, &Error{Code: "LAUNCH_FAILED", Stage: "launching-script-runner", Cause: err}
	}
	observerChild, err := group.Start(spec.ObserverExecutable)
	if err != nil {
		return Result{}, &Error{Code: "LAUNCH_FAILED", Stage: "launching-observer", Cause: err}
	}
	scriptDiagnostics := startDiagnostics(scriptChild.Stderr, 32<<10)
	observerDiagnostics := startDiagnostics(observerChild.Stderr, 32<<10)
	scriptConn, err := observationprotocol.NewConn(scriptChild.Stdout, scriptChild.Stdin, observationprotocol.DefaultMaxFrameBytes)
	if err != nil {
		return Result{}, err
	}
	observerConn, err := observationprotocol.NewConn(observerChild.Stdout, observerChild.Stdin, observationprotocol.DefaultMaxFrameBytes)
	if err != nil {
		return Result{}, err
	}

	if err := initializeObserver(observerConn, spec, pkg, blobRoot); err != nil {
		return Result{}, withDiagnostics("OBSERVER_INITIALIZE_FAILED", "initializing-observer", err, observerDiagnostics)
	}
	if err := initializeScriptRunner(scriptConn, spec, pkg); err != nil {
		return Result{}, withDiagnostics("SCRIPT_INITIALIZE_FAILED", "initializing-script-runner", err, scriptDiagnostics)
	}
	runParams, _ := json.Marshal(map[string]any{"jobId": spec.JobID, "inputs": spec.Inputs})
	if err := scriptConn.Write(observationprotocol.Message{ID: "run-1", Method: "script/run", Params: runParams}); err != nil {
		return Result{}, &Error{Code: "SCRIPT_PROTOCOL_INVALID", Stage: "starting-script", Cause: err}
	}
	output, provenance, scriptFailure, err := brokerUntilResult(
		runContext, spec.JobID, pkg, scriptConn, observerConn, blobs,
	)
	if err != nil {
		var transport *scriptRunnerTransportError
		if errors.As(err, &transport) {
			return Result{}, withNamedDiagnostics(
				"SCRIPT_PROCESS_FAILED", "reading-script-runner", err,
				scriptDiagnostics, observerDiagnostics,
			)
		}
		return Result{}, withNamedDiagnostics(
			"JOB_BROKER_FAILED", "brokering-observer-calls", err,
			scriptDiagnostics, observerDiagnostics,
		)
	}

	if scriptFailure == nil {
		if err := shutdown(scriptConn, "script-shutdown", spec.JobID); err != nil {
			return Result{}, &Error{Code: "SCRIPT_PROTOCOL_INVALID", Stage: "shutting-down-script-runner", Cause: err}
		}
	}
	if err := shutdown(observerConn, "observer-shutdown", spec.JobID); err != nil {
		return Result{}, &Error{Code: "OBSERVER_PROTOCOL_INVALID", Stage: "shutting-down-observer", Cause: err}
	}
	scriptChild.Stdin.Close()
	observerChild.Stdin.Close()
	if code, err := scriptChild.Wait(); err != nil || code != 0 {
		return Result{}, withDiagnostics("SCRIPT_EXIT_FAILED", "waiting-script-runner", childExitError(code, err), scriptDiagnostics)
	}
	if code, err := observerChild.Wait(); err != nil || code != 0 {
		return Result{}, withDiagnostics("OBSERVER_EXIT_FAILED", "waiting-observer", childExitError(code, err), observerDiagnostics)
	}
	if scriptFailure != nil {
		var failure struct {
			Code  string `json:"code"`
			Stage string `json:"stage"`
		}
		if err := json.Unmarshal(output, &failure); err != nil || failure.Code == "" {
			return Result{}, &Error{Code: "SCRIPT_PROTOCOL_INVALID", Stage: "decoding-script-error", Cause: errors.New("script error data is invalid")}
		}
		return Result{}, &Error{
			Code:  failure.Code,
			Stage: failure.Stage,
			Cause: fmt.Errorf("%s: %s", scriptFailure.Message, output),
		}
	}
	return Result{Package: pkg.Identity, Output: output, Provenance: provenance}, nil
}

func validateBindings(pkg *scriptpackage.Package, spec Spec) error {
	if err := pkg.ValidateInputs(spec.Inputs); err != nil {
		return err
	}
	if spec.Process == nil {
		return errors.New("owning Rule process identity is required")
	}
	if memory := pkg.Manifest.Permissions.Memory; memory != nil {
		if memory.Target != "rule/current-process" {
			return fmt.Errorf("unsupported memory target %q", memory.Target)
		}
	}
	file := pkg.Manifest.Permissions.File
	if file == nil {
		if len(spec.FileRoots) != 0 {
			return errors.New("file roots are forbidden without file permission")
		}
		return nil
	}
	if len(spec.FileRoots) != len(file.Roots) {
		return fmt.Errorf("file root binding count is %d, want %d", len(spec.FileRoots), len(file.Roots))
	}
	for _, alias := range file.Roots {
		root, ok := spec.FileRoots[alias]
		if !ok {
			return fmt.Errorf("missing file root binding %q", alias)
		}
		if root == "" || !filepath.IsAbs(root) {
			return fmt.Errorf("file root binding %q must be absolute", alias)
		}
	}
	for alias := range spec.FileRoots {
		found := false
		for _, declared := range file.Roots {
			if alias == declared {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("undeclared file root binding %q", alias)
		}
	}
	return nil
}

func snapshotPackage(source string) (string, error) {
	if source == "" || !filepath.IsAbs(source) {
		return "", errors.New("script package root must be absolute")
	}
	canonicalSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", fmt.Errorf("resolve script package root: %w", err)
	}
	parent, err := os.MkdirTemp("", "windowsagent-script-snapshot-")
	if err != nil {
		return "", fmt.Errorf("create script snapshot root: %w", err)
	}
	target := filepath.Join(parent, "package")
	if err := os.Mkdir(target, 0o700); err != nil {
		os.RemoveAll(parent)
		return "", fmt.Errorf("create script snapshot package: %w", err)
	}
	err = filepath.WalkDir(canonicalSource, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(canonicalSource, name)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("script package symlink is forbidden: %s", relative)
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.Mkdir(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("script package member is not a regular file: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxScriptSnapshotMemberBytes {
			return fmt.Errorf(
				"script package member %s exceeds %d bytes",
				relative,
				maxScriptSnapshotMemberBytes,
			)
		}
		input, err := os.Open(name)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
	if err != nil {
		os.RemoveAll(parent)
		return "", fmt.Errorf("copy script package snapshot: %w", err)
	}
	return target, nil
}

func initializeObserver(conn *observationprotocol.Conn, spec Spec, pkg *scriptpackage.Package, blobRoot string) error {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": observationapi.ProtocolVersion,
		"jobId":           spec.JobID,
		"deadline":        spec.Deadline,
		"permissions":     pkg.Manifest.Permissions,
		"process":         spec.Process,
		"fileRoots":       spec.FileRoots,
		"blobRoot":        blobRoot,
	})
	if err := conn.Write(observationprotocol.Message{ID: "observer-initialize", Method: "initialize", Params: params}); err != nil {
		return err
	}
	response, err := conn.Read()
	if err != nil {
		return err
	}
	return responseFailure(response)
}

func initializeScriptRunner(conn *observationprotocol.Conn, spec Spec, pkg *scriptpackage.Package) error {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": observationapi.ProtocolVersion,
		"jobId":           spec.JobID,
		"deadline":        spec.Deadline,
		"package":         pkg.Identity,
	})
	if err := conn.Write(observationprotocol.Message{ID: "script-initialize", Method: "initialize", Params: params}); err != nil {
		return err
	}
	response, err := conn.Read()
	if err != nil {
		return err
	}
	return responseFailure(response)
}

func brokerUntilResult(
	ctx context.Context,
	jobID string,
	pkg *scriptpackage.Package,
	scriptConn, observerConn *observationprotocol.Conn,
	blobs *blobCatalog,
) (json.RawMessage, []Provenance, *observationprotocol.ResponseError, error) {
	var provenance []Provenance
	lastCall := "none"
	for {
		if err := ctx.Err(); err != nil {
			return nil, provenance, nil, err
		}
		message, err := scriptConn.Read()
		if err != nil {
			return nil, provenance, nil, &scriptRunnerTransportError{after: lastCall, cause: err}
		}
		if message.Method == "" && message.ID == "run-1" {
			if message.Error != nil {
				return append(json.RawMessage(nil), message.Error.Data...), provenance, message.Error, nil
			}
			return append(json.RawMessage(nil), message.Result...), provenance, nil, nil
		}
		if message.Method == "broker/blobPath" {
			var params struct {
				JobID string         `json:"jobId"`
				Blob  map[string]any `json:"blob"`
			}
			if err := decodeStrict(message.Params, &params); err != nil || params.JobID != jobID {
				return nil, provenance, nil, errors.New("script-runner blob path request is invalid")
			}
			path, err := blobs.path(params.Blob)
			if err != nil {
				data, _ := json.Marshal(map[string]any{"kind": "NATIVE_BLOB_INVALID"})
				if writeErr := scriptConn.Write(observationprotocol.Message{
					ID: message.ID,
					Error: &observationprotocol.ResponseError{
						Code: -32050, Message: err.Error(), Data: data,
					},
				}); writeErr != nil {
					return nil, provenance, nil, writeErr
				}
				continue
			}
			result, _ := json.Marshal(map[string]any{"path": path})
			if err := scriptConn.Write(observationprotocol.Message{ID: message.ID, Result: result}); err != nil {
				return nil, provenance, nil, err
			}
			continue
		}
		if message.Method == "broker/nativeRecord" {
			var params struct {
				JobID  string                    `json:"jobId"`
				Record scriptrunner.NativeRecord `json:"record"`
			}
			if err := decodeStrict(message.Params, &params); err != nil ||
				params.JobID != jobID || params.Record.Alias == "" ||
				params.Record.Action == "" || params.Record.Phase == "" {
				return nil, provenance, nil, errors.New("script-runner native provenance record is invalid")
			}
			record := params.Record
			library, declared := pkg.Manifest.NativeLibraries[record.Alias]
			if !declared ||
				(record.Action != "load" && record.Action != "bind" && record.Action != "call") ||
				(record.Phase != "started" && record.Phase != "completed" && record.Phase != "failed") ||
				record.CallsUsed > library.MaxCalls ||
				record.NativeMemoryBytes > library.MaxNativeMemoryBytes ||
				record.ResultBytes > pkg.Manifest.Limits.MaxResultBytes {
				return nil, provenance, nil, errors.New("script-runner native provenance exceeds validated package contract")
			}
			provenance = append(provenance, Provenance{
				Namespace: "native", Operation: record.Action,
				Native: &record, ErrorKind: record.ErrorKind,
			})
			result, _ := json.Marshal(map[string]any{"recorded": true})
			if err := scriptConn.Write(observationprotocol.Message{ID: message.ID, Result: result}); err != nil {
				return nil, provenance, nil, err
			}
			continue
		}
		if message.Method != "broker/call" {
			return nil, provenance, nil, fmt.Errorf("unexpected script-runner message %q", message.Method)
		}
		var call observationapi.Call
		if err := decodeStrict(message.Params, &call); err != nil {
			return nil, provenance, nil, err
		}
		if err := call.Validate(); err != nil || call.JobID != jobID {
			return nil, provenance, nil, errors.New("script-runner broker call is invalid")
		}
		lastCall = call.Namespace + "." + call.Operation
		if err := observerConn.Write(observationprotocol.Message{
			ID: message.ID, Method: "observer/call", Params: message.Params,
		}); err != nil {
			return nil, provenance, nil, err
		}
		response, err := observerConn.Read()
		if err != nil {
			return nil, provenance, nil, fmt.Errorf("read observer response for %s: %w", lastCall, err)
		}
		if response.ID != message.ID || response.Method != "" {
			return nil, provenance, nil, errors.New("observer response does not match broker call")
		}
		record := Provenance{
			ObserverCallID: call.ObserverCallID,
			Namespace:      call.Namespace,
			Operation:      call.Operation,
		}
		if response.Error != nil {
			var typed observationapi.Error
			if err := decodeStrict(response.Error.Data, &typed); err != nil {
				return nil, provenance, nil, err
			}
			record.ErrorKind = typed.Kind
		} else {
			var result observationapi.Result
			if err := decodeStrict(response.Result, &result); err != nil {
				return nil, provenance, nil, err
			}
			if result.JobID != jobID || result.ObserverCallID != call.ObserverCallID ||
				result.Namespace != call.Namespace || result.Operation != call.Operation {
				return nil, provenance, nil, errors.New("observer result envelope does not match broker call")
			}
			record.ObservedAt = &result.ObservedAt
			record.Accounting = &result.Accounting
			if call.Namespace == observationapi.NamespaceFile && call.Operation == "openBlob" {
				if err := blobs.registerObserverValue(result.Value); err != nil {
					return nil, provenance, nil, fmt.Errorf("register job blob: %w", err)
				}
			}
		}
		provenance = append(provenance, record)
		if err := scriptConn.Write(observationprotocol.Message{
			ID: message.ID, Result: response.Result, Error: response.Error,
		}); err != nil {
			return nil, provenance, nil, err
		}
	}
}

type scriptRunnerTransportError struct {
	after string
	cause error
}

func (e *scriptRunnerTransportError) Error() string {
	return fmt.Sprintf("read script-runner message after %s: %v", e.after, e.cause)
}

func (e *scriptRunnerTransportError) Unwrap() error { return e.cause }

func shutdown(conn *observationprotocol.Conn, id, jobID string) error {
	params, _ := json.Marshal(map[string]any{"jobId": jobID})
	if err := conn.Write(observationprotocol.Message{ID: id, Method: "shutdown", Params: params}); err != nil {
		return err
	}
	response, err := conn.Read()
	if err != nil {
		return err
	}
	return responseFailure(response)
}

func responseFailure(response observationprotocol.Message) error {
	if response.Error == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", response.Error.Message, response.Error.Data)
}

func decodeStrict(data json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	return decoder.Decode(target)
}

type boundedBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
	done  chan struct{}
}

func startDiagnostics(reader io.Reader, limit int) *boundedBuffer {
	buffer := &boundedBuffer{limit: limit, done: make(chan struct{})}
	go func() {
		_, _ = io.Copy(buffer, reader)
		close(buffer.done)
	}()
	return buffer
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(remaining, len(data))]...)
	}
	return len(data), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func withDiagnostics(code, stage string, cause error, diagnostics ...*boundedBuffer) error {
	message := cause.Error()
	for _, diagnostic := range diagnostics {
		if text := diagnostic.String(); text != "" {
			message += "; stderr=" + text
		}
	}
	return &Error{Code: code, Stage: stage, Cause: errors.New(message)}
}

func withNamedDiagnostics(code, stage string, cause error, script, observer *boundedBuffer) error {
	message := cause.Error()
	for _, diagnostic := range []struct {
		name   string
		buffer *boundedBuffer
	}{
		{name: "script-runner", buffer: script},
		{name: "observer", buffer: observer},
	} {
		if text := diagnostic.buffer.String(); text != "" {
			message += "; " + diagnostic.name + " stderr=" + text
		}
	}
	return &Error{Code: code, Stage: stage, Cause: errors.New(message)}
}

func childExitError(code uint32, err error) error {
	if err != nil {
		return fmt.Errorf("exitCode=%d: %w", code, err)
	}
	return fmt.Errorf("exitCode=%d", code)
}
