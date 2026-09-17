//go:build windows

package windowsexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (OSExecutor) Execute(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, &Error{Code: "EXEC_CONTEXT_REQUIRED", Stage: "validating-request", Cause: errors.New("execution context is required")}
	}
	request, err := validateRequest(request)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, contextExecutionError("validating-request", err)
	}
	if request.TimeoutMilliseconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMilliseconds)*time.Millisecond)
		defer cancel()
	}

	executable, argv, err := resolveCommand(request)
	if err != nil {
		return Result{}, err
	}
	if request.Operation == OperationStart {
		return executeDetached(ctx, request, executable, argv)
	}
	return executeOwned(ctx, request, executable, argv)
}

func resolveCommand(request Request) (string, []string, error) {
	if request.Operation == OperationPowerShellFile {
		windowsDirectory, err := windows.GetWindowsDirectory()
		if err != nil {
			return "", nil, &Error{Code: "EXEC_POWERSHELL_RESOLVE_FAILED", Stage: "resolving-powershell", Cause: err}
		}
		executable := filepath.Join(windowsDirectory, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if err := requireRegularFile(executable, "built-in Windows PowerShell executable"); err != nil {
			return "", nil, &Error{Code: "EXEC_POWERSHELL_RESOLVE_FAILED", Stage: "resolving-powershell", Cause: err}
		}
		if err := requireRegularFile(request.ScriptPath, "PowerShell script"); err != nil {
			return "", nil, &Error{Code: "EXEC_SCRIPT_NOT_FOUND", Stage: "resolving-script", Cause: err}
		}
		return executable, powerShellArguments(request.ScriptPath, request.Argv), nil
	}

	executable, err := exec.LookPath(request.Executable)
	if err != nil {
		return "", nil, &Error{Code: "EXEC_EXECUTABLE_RESOLVE_FAILED", Stage: "resolving-executable", Cause: err}
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", nil, &Error{Code: "EXEC_EXECUTABLE_RESOLVE_FAILED", Stage: "resolving-executable", Cause: err}
	}
	executable = filepath.Clean(executable)
	if err := requireRegularFile(executable, "executable"); err != nil {
		return "", nil, &Error{Code: "EXEC_EXECUTABLE_RESOLVE_FAILED", Stage: "resolving-executable", Cause: err}
	}
	return executable, request.Argv, nil
}

func requireRegularFile(path, description string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s %q: %w", description, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file: %q", description, path)
	}
	return nil
}

type processReceipt struct {
	pid          uint32
	creationTime time.Time
	sessionID    uint32
	startedAt    time.Time
	executable   string
}

func executeDetached(ctx context.Context, request Request, executable string, argv []string) (result Result, resultErr error) {
	if err := ctx.Err(); err != nil {
		return Result{}, contextExecutionError("starting-process", err)
	}
	nul, err := os.OpenFile("NUL", os.O_RDWR, 0)
	if err != nil {
		return Result{}, &Error{Code: "EXEC_STANDARD_HANDLE_FAILED", Stage: "preparing-process", Cause: err}
	}
	defer nul.Close()
	handles := []windows.Handle{windows.Handle(nul.Fd())}
	process, receipt, err := createSuspendedProcess(request, executable, argv, handles[0], handles[0], handles[0],
		0)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		windows.CloseHandle(process.Thread)
		windows.CloseHandle(process.Process)
	}()
	if err := ctx.Err(); err != nil {
		if terminateErr := windows.TerminateProcess(process.Process, 1); terminateErr != nil {
			return Result{}, &Error{Code: "EXEC_CANCEL_FAILED", Stage: "cancelling-process", Cause: errors.Join(err, terminateErr)}
		}
		return Result{}, contextExecutionError("starting-process", err)
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		terminateErr := windows.TerminateProcess(process.Process, 1)
		return Result{}, &Error{Code: "EXEC_PROCESS_RESUME_FAILED", Stage: "resuming-process", Cause: errors.Join(err, terminateErr)}
	}
	return receiptResult(request.Operation, receipt), nil
}

func executeOwned(ctx context.Context, request Request, executable string, argv []string) (_ Result, resultErr error) {
	job, err := createKillJob()
	if err != nil {
		return Result{}, err
	}
	jobOpen := true
	defer func() {
		if jobOpen {
			_ = windows.CloseHandle(job)
		}
	}()

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return Result{}, &Error{Code: "EXEC_PIPE_CREATE_FAILED", Stage: "preparing-process", Cause: err}
	}
	defer stdinWrite.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		return Result{}, &Error{Code: "EXEC_PIPE_CREATE_FAILED", Stage: "preparing-process", Cause: err}
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		return Result{}, &Error{Code: "EXEC_PIPE_CREATE_FAILED", Stage: "preparing-process", Cause: err}
	}
	defer stderrRead.Close()

	process, receipt, err := createSuspendedProcess(
		request, executable, argv,
		windows.Handle(stdinRead.Fd()), windows.Handle(stdoutWrite.Fd()), windows.Handle(stderrWrite.Fd()), 0,
	)
	stdinRead.Close()
	stdoutWrite.Close()
	stderrWrite.Close()
	if err != nil {
		return Result{}, err
	}
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return Result{}, &Error{Code: "EXEC_JOB_ASSIGN_FAILED", Stage: "assigning-process-job", Cause: err}
	}
	if err := ctx.Err(); err != nil {
		terminateErr, closeErr := terminateAndCloseJob(job)
		if closeErr == nil {
			jobOpen = false
		}
		if terminateErr != nil || closeErr != nil {
			return Result{}, &Error{Code: "EXEC_CANCEL_FAILED", Stage: "cancelling-process-tree", Cause: errors.Join(err, terminateErr, closeErr)}
		}
		return Result{}, contextExecutionError("starting-process", err)
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		return Result{}, &Error{Code: "EXEC_PROCESS_RESUME_FAILED", Stage: "resuming-process", Cause: err}
	}

	outputLimit := newOutputLimitSignal()
	stdout := &captureBuffer{limit: request.MaxOutputBytes, signal: outputLimit}
	stderr := &captureBuffer{limit: request.MaxOutputBytes, signal: outputLimit}
	type copyResult struct {
		stream string
		err    error
	}
	copyResults := make(chan copyResult, 2)
	go func() {
		_, copyErr := io.Copy(stdout, stdoutRead)
		copyResults <- copyResult{stream: "stdout", err: copyErr}
	}()
	go func() {
		_, copyErr := io.Copy(stderr, stderrRead)
		copyResults <- copyResult{stream: "stderr", err: copyErr}
	}()
	stdinResult := make(chan error, 1)
	go func() {
		_, writeErr := stdinWrite.Write(request.Stdin)
		closeErr := stdinWrite.Close()
		if writeErr != nil {
			stdinResult <- writeErr
			return
		}
		stdinResult <- closeErr
	}()

	waitResult := make(chan error, 1)
	go func() {
		status, waitErr := windows.WaitForSingleObject(process.Process, windows.INFINITE)
		if waitErr == nil && status != windows.WAIT_OBJECT_0 {
			waitErr = fmt.Errorf("unexpected wait status %d", status)
		}
		waitResult <- waitErr
	}()

	var waitErr error
	stopReason := ""
	select {
	case waitErr = <-waitResult:
	case <-outputLimit.exceeded:
		stopReason = "output-limit"
	case <-ctx.Done():
		select {
		case <-outputLimit.exceeded:
			stopReason = "output-limit"
		default:
			stopReason = "context"
		}
	}
	if stopReason != "" {
		terminateErr, closeErr := terminateAndCloseJob(job)
		if closeErr == nil {
			jobOpen = false
		}
		waitErr = <-waitResult
		for range 2 {
			<-copyResults
		}
		<-stdinResult
		if terminateErr != nil || closeErr != nil {
			stage := "cancelling-process-tree"
			cause := ctx.Err()
			limit := uint64(0)
			if stopReason == "output-limit" {
				stage = "enforcing-output-limit"
				cause = errors.New("stdout or stderr exceeded maxOutputBytes")
				limit = request.MaxOutputBytes
			}
			stdoutBytes, _ := stdout.stats()
			stderrBytes, _ := stderr.stats()
			return Result{}, &Error{
				Code: "EXEC_CANCEL_FAILED", Stage: stage, Cause: errors.Join(cause, terminateErr, closeErr),
				PID: receipt.pid, DurationMS: time.Since(receipt.startedAt).Milliseconds(),
				StdoutBytes: stdoutBytes, StderrBytes: stderrBytes, OutputLimitBytes: limit,
			}
		}
		if stopReason == "output-limit" {
			return Result{}, outputLimitError(receipt, stdout, stderr, request.MaxOutputBytes)
		}
		return Result{}, processContextError("waiting-for-process", ctx.Err(), receipt, stdout, stderr)
	}
	if waitErr != nil {
		return Result{}, &Error{Code: "EXEC_PROCESS_WAIT_FAILED", Stage: "waiting-for-process", Cause: waitErr}
	}
	if err := windows.CloseHandle(job); err != nil {
		return Result{}, &Error{Code: "EXEC_JOB_CLOSE_FAILED", Stage: "closing-process-job", Cause: err}
	}
	jobOpen = false

	if err := <-stdinResult; err != nil {
		return Result{}, &Error{Code: "EXEC_STDIN_WRITE_FAILED", Stage: "writing-process-stdin", Cause: err}
	}
	for range 2 {
		copyResult := <-copyResults
		if copyResult.err != nil {
			return Result{}, &Error{Code: "EXEC_OUTPUT_READ_FAILED", Stage: "reading-process-" + copyResult.stream, Cause: copyResult.err}
		}
	}
	_, stdoutExceeded := stdout.stats()
	_, stderrExceeded := stderr.stats()
	if stdoutExceeded || stderrExceeded {
		return Result{}, outputLimitError(receipt, stdout, stderr, request.MaxOutputBytes)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return Result{}, &Error{Code: "EXEC_EXIT_CODE_FAILED", Stage: "reading-process-exit", Cause: err}
	}
	completedAt := time.Now().UTC()
	signedExitCode := int(int32(exitCode))
	result := receiptResult(request.Operation, receipt)
	result.CompletedAt = &completedAt
	result.DurationMS = completedAt.Sub(receipt.startedAt).Milliseconds()
	result.ExitCode = &signedExitCode
	result.Stdout = encodeOutput(stdout.Bytes())
	result.Stderr = encodeOutput(stderr.Bytes())
	return result, nil
}

func terminateAndCloseJob(job windows.Handle) (terminateErr, closeErr error) {
	terminateErr = windows.TerminateJobObject(job, 1)
	closeErr = windows.CloseHandle(job)
	return terminateErr, closeErr
}

func createKillJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, &Error{Code: "EXEC_JOB_CREATE_FAILED", Stage: "creating-process-job", Cause: err}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return 0, &Error{Code: "EXEC_JOB_CONFIGURE_FAILED", Stage: "configuring-process-job", Cause: err}
	}
	return job, nil
}

func createSuspendedProcess(
	request Request,
	executable string,
	argv []string,
	stdin windows.Handle,
	stdout windows.Handle,
	stderr windows.Handle,
	extraFlags uint32,
) (_ windows.ProcessInformation, _ processReceipt, resultErr error) {
	childHandles := uniqueHandles(stdin, stdout, stderr)
	for _, handle := range childHandles {
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_HANDLE_CONFIGURE_FAILED", Stage: "preparing-process", Cause: err}
		}
	}
	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_ATTRIBUTE_LIST_FAILED", Stage: "preparing-process", Cause: err}
	}
	defer attributeList.Delete()
	if err := attributeList.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&childHandles[0]),
		uintptr(len(childHandles))*unsafe.Sizeof(childHandles[0]),
	); err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_HANDLE_LIST_FAILED", Stage: "preparing-process", Cause: err}
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  stdin,
			StdOutput: stdout,
			StdErr:    stderr,
		},
		ProcThreadAttributeList: attributeList.List(),
	}
	if request.Window == WindowHidden {
		startup.Flags |= windows.STARTF_USESHOWWINDOW
		startup.ShowWindow = windows.SW_HIDE
	}
	appName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_COMMAND_LINE_FAILED", Stage: "preparing-process", Cause: err}
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{executable}, argv...)))
	if err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_COMMAND_LINE_FAILED", Stage: "preparing-process", Cause: err}
	}
	environment, err := environmentBlock(request.Env)
	if err != nil {
		return windows.ProcessInformation{}, processReceipt{}, err
	}
	var cwd *uint16
	if request.Cwd != "" {
		cwd, err = windows.UTF16PtrFromString(request.Cwd)
		if err != nil {
			return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_CWD_FAILED", Stage: "preparing-process", Cause: err}
		}
	}
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if request.Window == WindowHidden {
		flags |= windows.CREATE_NO_WINDOW
	}
	flags |= extraFlags
	startedAt := time.Now().UTC()
	var process windows.ProcessInformation
	if err := windows.CreateProcess(
		appName, commandLine, nil, nil, true, flags, &environment[0], cwd,
		&startup.StartupInfo, &process,
	); err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_PROCESS_START_FAILED", Stage: "starting-process", Cause: err}
	}
	defer func() {
		if resultErr != nil {
			_ = windows.TerminateProcess(process.Process, 1)
			windows.CloseHandle(process.Thread)
			windows.CloseHandle(process.Process)
		}
	}()
	creationTime, err := processCreationTime(process.Process)
	if err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_PROCESS_EVIDENCE_FAILED", Stage: "reading-process-identity", Cause: err}
	}
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(process.ProcessId, &sessionID); err != nil {
		return windows.ProcessInformation{}, processReceipt{}, &Error{Code: "EXEC_PROCESS_EVIDENCE_FAILED", Stage: "reading-process-identity", Cause: err}
	}
	return process, processReceipt{
		pid: process.ProcessId, creationTime: creationTime, sessionID: sessionID,
		startedAt: startedAt, executable: executable,
	}, nil
}

func processCreationTime(process windows.Handle) (time.Time, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, creation.Nanoseconds()).UTC(), nil
}

func uniqueHandles(handles ...windows.Handle) []windows.Handle {
	unique := make([]windows.Handle, 0, len(handles))
	seen := make(map[windows.Handle]struct{}, len(handles))
	for _, handle := range handles {
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		unique = append(unique, handle)
	}
	return unique
}

func receiptResult(operation Operation, receipt processReceipt) Result {
	return Result{
		SchemaVersion: SchemaVersion, Runtime: RuntimeID, Operation: operation,
		PID: receipt.pid, CreationTime: receipt.creationTime, SessionID: receipt.sessionID,
		Executable: receipt.executable, StartedAt: receipt.startedAt,
		Stdout: encodeOutput(nil), Stderr: encodeOutput(nil),
	}
}

func environmentBlock(overrides map[string]string) ([]uint16, error) {
	type value struct {
		name  string
		value string
	}
	values := make(map[string]value)
	for _, item := range os.Environ() {
		name, itemValue, ok := splitEnvironmentItem(item)
		if !ok {
			return nil, &Error{Code: "EXEC_ENVIRONMENT_FAILED", Stage: "preparing-process", Cause: fmt.Errorf("malformed inherited environment entry %q", name)}
		}
		values[strings.ToUpper(name)] = value{name: name, value: itemValue}
	}
	for name, itemValue := range overrides {
		values[strings.ToUpper(name)] = value{name: name, value: itemValue}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	block := make([]rune, 0)
	for _, key := range keys {
		item := values[key]
		block = append(block, []rune(item.name+"="+item.value)...)
		block = append(block, 0)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	return utf16.Encode(block), nil
}

func splitEnvironmentItem(item string) (string, string, bool) {
	separator := strings.IndexByte(item, '=')
	if separator == 0 && strings.HasPrefix(item, "=") {
		separator = strings.IndexByte(item[1:], '=')
		if separator >= 0 {
			separator++
		}
	}
	if separator < 0 {
		return item, "", false
	}
	return item[:separator], item[separator+1:], true
}

func contextExecutionError(stage string, err error) *Error {
	code := "EXEC_CANCELLED"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "EXEC_DEADLINE_EXCEEDED"
	}
	return &Error{Code: code, Stage: stage, Cause: err}
}

func processContextError(stage string, err error, receipt processReceipt, stdout, stderr *captureBuffer) error {
	result := contextExecutionError(stage, err)
	result.PID = receipt.pid
	result.DurationMS = time.Since(receipt.startedAt).Milliseconds()
	result.StdoutBytes, _ = stdout.stats()
	result.StderrBytes, _ = stderr.stats()
	return result
}

func outputLimitError(receipt processReceipt, stdout, stderr *captureBuffer, limit uint64) error {
	stdoutBytes, _ := stdout.stats()
	stderrBytes, _ := stderr.stats()
	return &Error{
		Code: "EXEC_OUTPUT_LIMIT_EXCEEDED", Stage: "capturing-process-output",
		Cause: fmt.Errorf("stdout or stderr exceeded maxOutputBytes=%d: stdoutBytes=%d stderrBytes=%d", limit, stdoutBytes, stderrBytes),
		PID:   receipt.pid, DurationMS: time.Since(receipt.startedAt).Milliseconds(),
		StdoutBytes: stdoutBytes, StderrBytes: stderrBytes, OutputLimitBytes: limit,
	}
}
