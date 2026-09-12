package windowsautomation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Error struct {
	Code  string
	Stage string
	Cause error
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

type ProcessRequest struct {
	Executable     string
	Argv           []string
	Cwd            string
	Env            map[string]string
	Stdin          string
	MaxOutputBytes uint64
}

type ProcessResult struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	PID        int    `json:"pid"`
	DurationMS int64  `json:"durationMs"`
}

type ProcessHost interface {
	Run(context.Context, ProcessRequest) (ProcessResult, error)
}

type FileInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	IsDir      bool   `json:"isDir"`
	ModifiedAt string `json:"modifiedAt"`
}

type FileHost interface {
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	Stat(context.Context, string) (FileInfo, error)
	List(context.Context, string) ([]FileInfo, error)
	Mkdir(context.Context, string, bool) error
	Copy(context.Context, string, string, bool) error
	Move(context.Context, string, string, bool) error
	Remove(context.Context, string, bool) error
}

type OSProcessHost struct{}

func (OSProcessHost) Run(ctx context.Context, request ProcessRequest) (ProcessResult, error) {
	if ctx == nil {
		return ProcessResult{}, errors.New("process context is required")
	}
	if request.Executable == "" || strings.TrimSpace(request.Executable) != request.Executable {
		return ProcessResult{}, errors.New("process executable is required and must be canonical")
	}
	if request.MaxOutputBytes == 0 {
		return ProcessResult{}, errors.New("process output limit must be positive")
	}
	command := exec.CommandContext(ctx, request.Executable, request.Argv...)
	command.Dir = request.Cwd
	command.Env = mergedEnvironment(request.Env)
	command.Stdin = strings.NewReader(request.Stdin)
	stdout := &boundedBuffer{limit: request.MaxOutputBytes}
	stderr := &boundedBuffer{limit: request.MaxOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	started := time.Now()
	if err := command.Start(); err != nil {
		return ProcessResult{}, &Error{Code: "PROCESS_START_FAILED", Stage: "starting-process", Cause: err}
	}
	pid := command.Process.Pid
	err := command.Wait()
	if stdout.exceeded || stderr.exceeded {
		return ProcessResult{}, &Error{Code: "PROCESS_OUTPUT_LIMIT_EXCEEDED", Stage: "capturing-process-output", Cause: errors.New("stdout or stderr exceeded maxProcessOutputBytes")}
	}
	if ctx.Err() != nil {
		return ProcessResult{}, &Error{Code: cancellationCode(ctx.Err()), Stage: "waiting-for-process", Cause: ctx.Err()}
	}
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return ProcessResult{}, &Error{Code: "PROCESS_WAIT_FAILED", Stage: "waiting-for-process", Cause: err}
	}
	if command.ProcessState == nil {
		return ProcessResult{}, &Error{Code: "PROCESS_WAIT_FAILED", Stage: "waiting-for-process", Cause: errors.New("process completed without an exit state")}
	}
	result := ProcessResult{
		ExitCode: command.ProcessState.ExitCode(), Stdout: stdout.String(), Stderr: stderr.String(), PID: pid,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if !utf8.ValidString(result.Stdout) || !utf8.ValidString(result.Stderr) {
		return ProcessResult{}, &Error{Code: "PROCESS_OUTPUT_NOT_UTF8", Stage: "capturing-process-output", Cause: errors.New("stdout and stderr must be UTF-8")}
	}
	return result, nil
}

type boundedBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    uint64
	exceeded bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if uint64(b.data.Len()+len(data)) > b.limit {
		remaining := b.limit - uint64(b.data.Len())
		if remaining > 0 {
			_, _ = b.data.Write(data[:int(remaining)])
		}
		b.exceeded = true
		return int(remaining), errors.New("process output limit exceeded")
	}
	return b.data.Write(data)
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func mergedEnvironment(overrides map[string]string) []string {
	values := map[string]string{}
	names := map[string]string{}
	keyFor := func(name string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(name)
		}
		return name
	}
	for _, item := range os.Environ() {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key := keyFor(name)
		names[key], values[key] = name, value
	}
	for name, value := range overrides {
		key := keyFor(name)
		names[key], values[key] = name, value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, names[key]+"="+values[key])
	}
	return result
}

type OSFileHost struct{}

func (OSFileHost) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

func (OSFileHost) WriteFile(ctx context.Context, name string, data []byte) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o600)
}

func (OSFileHost) Stat(ctx context.Context, name string) (FileInfo, error) {
	if err := contextError(ctx); err != nil {
		return FileInfo{}, err
	}
	info, err := os.Stat(name)
	if err != nil {
		return FileInfo{}, err
	}
	return fileInfo(name, info), nil
}

func (OSFileHost) List(ctx context.Context, name string) ([]FileInfo, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		result = append(result, fileInfo(filepath.Join(name, entry.Name()), info))
	}
	return result, nil
}

func (OSFileHost) Mkdir(ctx context.Context, name string, parents bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if parents {
		return os.MkdirAll(name, 0o700)
	}
	return os.Mkdir(name, 0o700)
}

func (OSFileHost) Copy(ctx context.Context, source, destination string, overwrite bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copy source is a symlink: %s", source)
	}
	if err := prepareDestination(destination, overwrite); err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirectory(ctx, source, destination, info.Mode())
	}
	return copyRegularFile(ctx, source, destination, info.Mode())
}

func (OSFileHost) Move(ctx context.Context, source, destination string, overwrite bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := prepareDestination(destination, overwrite); err != nil {
		return err
	}
	return os.Rename(source, destination)
}

func (OSFileHost) Remove(ctx context.Context, name string, recursive bool) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if _, err := os.Lstat(name); err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(name)
	}
	return os.Remove(name)
}

func prepareDestination(name string, overwrite bool) error {
	_, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !overwrite {
		return fs.ErrExist
	}
	return os.RemoveAll(name)
}

func copyDirectory(ctx context.Context, source, destination string, mode fs.FileMode) error {
	if err := os.Mkdir(destination, mode.Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := contextError(ctx); err != nil {
			return err
		}
		from, to := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy source contains symlink: %s", from)
		}
		if info.IsDir() {
			err = copyDirectory(ctx, from, to, info.Mode())
		} else if info.Mode().IsRegular() {
			err = copyRegularFile(ctx, from, to, info.Mode())
		} else {
			err = fmt.Errorf("copy source is not a regular file or directory: %s", from)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(ctx context.Context, source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, &contextReader{ctx: ctx, reader: input})
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := contextError(r.ctx); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func fileInfo(name string, info fs.FileInfo) FileInfo {
	return FileInfo{
		Name: info.Name(), Path: name, Size: info.Size(), IsDir: info.IsDir(),
		ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	return ctx.Err()
}

func cancellationCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "ACTION_DEADLINE_EXCEEDED"
	}
	return "ACTION_CANCELLED"
}
