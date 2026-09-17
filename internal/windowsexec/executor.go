// Package windowsexec owns direct Windows process execution for the
// windows-exec-v1 runtime. It never interprets a shell command string.
package windowsexec

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const RuntimeID = "windows-exec-v1"
const SchemaVersion uint32 = 1

// Inline process output is carried inside one durable event whose encoded
// record is capped at 1 MiB. The 64 KiB per-stream limit leaves room for both
// the byte-safe base64 representation and worst-case JSON escaping of valid
// UTF-8 text from stdout and stderr, plus the invocation event envelope.
const DefaultMaxOutputBytes uint64 = 64 << 10
const MaxOutputBytes uint64 = DefaultMaxOutputBytes

type Operation string

const (
	OperationRun            Operation = "run"
	OperationStart          Operation = "start"
	OperationPowerShellFile Operation = "powershell-file"
)

type WindowMode string

const (
	WindowNormal WindowMode = "normal"
	WindowHidden WindowMode = "hidden"
)

type Request struct {
	SchemaVersion       uint32            `json:"schemaVersion"`
	Operation           Operation         `json:"operation"`
	Executable          string            `json:"executable,omitempty"`
	ScriptPath          string            `json:"scriptPath,omitempty"`
	Argv                []string          `json:"argv,omitempty"`
	Cwd                 string            `json:"cwd,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	Stdin               []byte            `json:"stdin,omitempty"`
	Window              WindowMode        `json:"window,omitempty"`
	MaxOutputBytes      uint64            `json:"maxOutputBytes,omitempty"`
	TimeoutMilliseconds uint64            `json:"timeoutMilliseconds,omitempty"`
}

type Output struct {
	BytesBase64 string  `json:"bytesBase64"`
	Text        *string `json:"text,omitempty"`
	ByteLength  int     `json:"byteLength"`
}

type Result struct {
	SchemaVersion uint32     `json:"schemaVersion"`
	Runtime       string     `json:"runtime"`
	Operation     Operation  `json:"operation"`
	PID           uint32     `json:"pid"`
	CreationTime  time.Time  `json:"creationTime"`
	SessionID     uint32     `json:"sessionId"`
	Executable    string     `json:"executable"`
	StartedAt     time.Time  `json:"startedAt"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
	DurationMS    int64      `json:"durationMs,omitempty"`
	ExitCode      *int       `json:"exitCode,omitempty"`
	Stdout        Output     `json:"stdout"`
	Stderr        Output     `json:"stderr"`
}

type Executor interface {
	Execute(context.Context, Request) (Result, error)
}

type OSExecutor struct{}

type Error struct {
	Code             string
	Stage            string
	Cause            error
	PID              uint32
	DurationMS       int64
	StdoutBytes      uint64
	StderrBytes      uint64
	OutputLimitBytes uint64
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s at %s: %v", e.Code, e.Stage, e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (r Request) Validate() error {
	_, err := validateRequest(r)
	return err
}

func validateRequest(request Request) (Request, error) {
	if request.SchemaVersion != SchemaVersion {
		return Request{}, requestError("EXEC_INVALID_SCHEMA_VERSION", "validating-request", "schemaVersion must be 1")
	}
	if request.Operation != OperationRun && request.Operation != OperationStart && request.Operation != OperationPowerShellFile {
		return Request{}, requestError("EXEC_INVALID_OPERATION", "validating-request", "operation must be run, start, or powershell-file")
	}
	if request.Window == "" {
		if request.Operation == OperationStart {
			request.Window = WindowNormal
		} else {
			request.Window = WindowHidden
		}
	}
	if request.Window != WindowNormal && request.Window != WindowHidden {
		return Request{}, requestError("EXEC_INVALID_WINDOW", "validating-request", "window must be normal or hidden")
	}
	for index, argument := range request.Argv {
		if strings.IndexByte(argument, 0) >= 0 {
			return Request{}, requestError("EXEC_INVALID_ARGUMENT", "validating-request", fmt.Sprintf("argv[%d] contains NUL", index))
		}
	}
	if strings.IndexByte(request.Cwd, 0) >= 0 {
		return Request{}, requestError("EXEC_INVALID_CWD", "validating-request", "cwd contains NUL")
	}
	if request.Cwd != "" && !isAbsoluteWindowsPath(request.Cwd) {
		return Request{}, requestError("EXEC_INVALID_CWD", "validating-request", "cwd must be an absolute Windows path")
	}
	for name, value := range request.Env {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return Request{}, requestError("EXEC_INVALID_ENVIRONMENT", "validating-request", fmt.Sprintf("environment variable name %q is invalid", name))
		}
		if strings.IndexByte(value, 0) >= 0 {
			return Request{}, requestError("EXEC_INVALID_ENVIRONMENT", "validating-request", fmt.Sprintf("environment variable %q contains NUL", name))
		}
	}
	environmentNames := make(map[string]string, len(request.Env))
	for name := range request.Env {
		key := strings.ToUpper(name)
		if existing, ok := environmentNames[key]; ok {
			return Request{}, requestError("EXEC_INVALID_ENVIRONMENT", "validating-request", fmt.Sprintf("environment variable names %q and %q differ only by case", existing, name))
		}
		environmentNames[key] = name
	}
	switch request.Operation {
	case OperationRun, OperationStart:
		if request.Executable == "" || strings.TrimSpace(request.Executable) != request.Executable || strings.IndexByte(request.Executable, 0) >= 0 {
			return Request{}, requestError("EXEC_INVALID_EXECUTABLE", "validating-request", "executable is required and must be canonical")
		}
		if request.ScriptPath != "" {
			return Request{}, requestError("EXEC_INVALID_SCRIPT_PATH", "validating-request", "scriptPath is valid only for powershell-file")
		}
	case OperationPowerShellFile:
		if request.ScriptPath == "" || strings.TrimSpace(request.ScriptPath) != request.ScriptPath || strings.IndexByte(request.ScriptPath, 0) >= 0 {
			return Request{}, requestError("EXEC_INVALID_SCRIPT_PATH", "validating-request", "scriptPath is required and must be canonical")
		}
		if request.Executable != "" {
			return Request{}, requestError("EXEC_INVALID_EXECUTABLE", "validating-request", "executable must be omitted for powershell-file")
		}
		if !isAbsoluteWindowsPath(request.ScriptPath) {
			return Request{}, requestError("EXEC_INVALID_SCRIPT_PATH", "validating-request", "scriptPath must be an absolute Windows path")
		}
		if !strings.EqualFold(scriptExtension(request.ScriptPath), ".ps1") {
			return Request{}, requestError("EXEC_INVALID_SCRIPT_PATH", "validating-request", "scriptPath must have a .ps1 extension")
		}
	}
	if request.Operation == OperationStart {
		if len(request.Stdin) != 0 {
			return Request{}, requestError("EXEC_INVALID_STDIN", "validating-request", "start does not accept stdin")
		}
		if request.MaxOutputBytes != 0 {
			return Request{}, requestError("EXEC_INVALID_OUTPUT_LIMIT", "validating-request", "start does not capture output")
		}
		if request.TimeoutMilliseconds != 0 {
			return Request{}, requestError("EXEC_INVALID_TIMEOUT", "validating-request", "start does not accept a timeout")
		}
	} else {
		if request.MaxOutputBytes == 0 {
			request.MaxOutputBytes = DefaultMaxOutputBytes
		}
		if request.MaxOutputBytes > MaxOutputBytes {
			return Request{}, requestError("EXEC_INVALID_OUTPUT_LIMIT", "validating-request", fmt.Sprintf("maxOutputBytes must not exceed %d", MaxOutputBytes))
		}
	}
	if request.TimeoutMilliseconds > uint64((1<<63-1)/int64(time.Millisecond)) {
		return Request{}, requestError("EXEC_INVALID_TIMEOUT", "validating-request", "timeoutMilliseconds exceeds the supported duration")
	}
	return request, nil
}

func isAbsoluteWindowsPath(value string) bool {
	if len(value) >= 3 && isASCIILetter(value[0]) && value[1] == ':' && isWindowsSeparator(value[2]) {
		return true
	}
	if len(value) < 5 || !isWindowsSeparator(value[0]) || !isWindowsSeparator(value[1]) {
		return false
	}
	rest := value[2:]
	serverEnd := strings.IndexAny(rest, `\\/`)
	if serverEnd <= 0 {
		return false
	}
	share := rest[serverEnd+1:]
	return share != "" && !isWindowsSeparator(share[0])
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isWindowsSeparator(value byte) bool { return value == '\\' || value == '/' }

func scriptExtension(value string) string {
	separator := strings.LastIndexAny(value, `\\/`)
	dot := strings.LastIndexByte(value, '.')
	if dot <= separator {
		return ""
	}
	return value[dot:]
}

func powerShellArguments(scriptPath string, argv []string) []string {
	arguments := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath}
	return append(arguments, argv...)
}

func requestError(code, stage, message string) error {
	return &Error{Code: code, Stage: stage, Cause: errors.New(message)}
}

func encodeOutput(data []byte) Output {
	result := Output{BytesBase64: base64.StdEncoding.EncodeToString(data), ByteLength: len(data)}
	if utf8.Valid(data) {
		text := string(data)
		result.Text = &text
	}
	return result
}

type outputLimitSignal struct {
	once     sync.Once
	exceeded chan struct{}
}

func newOutputLimitSignal() *outputLimitSignal {
	return &outputLimitSignal{exceeded: make(chan struct{})}
}

func (s *outputLimitSignal) trigger() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.exceeded) })
}

type captureBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    uint64
	observed uint64
	exceeded bool
	signal   *outputLimitSignal
}

func (b *captureBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	b.observed += uint64(len(data))
	current := uint64(b.data.Len())
	remaining := uint64(0)
	if current < b.limit {
		remaining = b.limit - current
	}
	writeLength := uint64(len(data))
	if writeLength > remaining {
		writeLength = remaining
	}
	if writeLength > 0 {
		_, _ = b.data.Write(data[:int(writeLength)])
	}
	if uint64(len(data)) > remaining {
		b.exceeded = true
	}
	exceeded := b.exceeded
	b.mu.Unlock()
	if exceeded {
		b.signal.trigger()
	}
	return len(data), nil
}

func (b *captureBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.data.Bytes())
}

func (b *captureBuffer) stats() (observed uint64, exceeded bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.observed, b.exceeded
}
