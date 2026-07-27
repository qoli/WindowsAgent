//go:build windows

// Package observationlauncher owns direct, no-console observer process launch.
package observationlauncher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Limits struct {
	ActiveProcesses    uint32
	ProcessMemoryBytes uintptr
	JobMemoryBytes     uintptr
}

type Group struct {
	mu     sync.Mutex
	job    windows.Handle
	closed bool
}

type Child struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	process windows.Handle
	pid     uint32
}

func NewGroup(limits Limits) (*Group, error) {
	if limits.ActiveProcesses == 0 || limits.ProcessMemoryBytes == 0 || limits.JobMemoryBytes == 0 {
		return nil, errors.New("active-process, process-memory, and job-memory limits are required")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create observation Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags =
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
			windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
			windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
			windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	info.BasicLimitInformation.ActiveProcessLimit = limits.ActiveProcesses
	info.ProcessMemoryLimit = limits.ProcessMemoryBytes
	info.JobMemoryLimit = limits.JobMemoryBytes
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure observation Job Object: %w", err)
	}
	return &Group{job: job}, nil
}

func (g *Group) Start(executable string, arguments ...string) (_ *Child, startErr error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, errors.New("observation launch group is closed")
	}
	if executable == "" || !filepath.IsAbs(executable) {
		return nil, errors.New("observer executable must be an absolute path")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return nil, fmt.Errorf("stat observer executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("observer executable must be a regular file")
	}

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		if startErr != nil {
			stdinRead.Close()
			stdinWrite.Close()
		}
	}()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		if startErr != nil {
			stdoutRead.Close()
			stdoutWrite.Close()
		}
	}()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		if startErr != nil {
			stderrRead.Close()
			stderrWrite.Close()
		}
	}()

	childHandles := []windows.Handle{
		windows.Handle(stdinRead.Fd()),
		windows.Handle(stdoutWrite.Fd()),
		windows.Handle(stderrWrite.Fd()),
	}
	for _, handle := range childHandles {
		if err := windows.SetHandleInformation(handle, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, fmt.Errorf("mark child pipe inheritable: %w", err)
		}
	}
	attributeList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attributeList.Delete()
	if err := attributeList.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&childHandles[0]),
		uintptr(len(childHandles))*unsafe.Sizeof(childHandles[0]),
	); err != nil {
		return nil, fmt.Errorf("restrict inherited handles: %w", err)
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  childHandles[0],
			StdOutput: childHandles[1],
			StdErr:    childHandles[2],
		},
		ProcThreadAttributeList: attributeList.List(),
	}
	appName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{executable}, arguments...)))
	if err != nil {
		return nil, err
	}
	var process windows.ProcessInformation
	flags := uint32(
		windows.CREATE_NO_WINDOW |
			windows.CREATE_SUSPENDED |
			windows.CREATE_UNICODE_ENVIRONMENT |
			windows.EXTENDED_STARTUPINFO_PRESENT,
	)
	if err := windows.CreateProcess(
		appName,
		commandLine,
		nil,
		nil,
		true,
		flags,
		nil,
		nil,
		&startup.StartupInfo,
		&process,
	); err != nil {
		return nil, fmt.Errorf("create observer process: %w", err)
	}
	defer windows.CloseHandle(process.Thread)
	defer func() {
		if startErr != nil {
			windows.TerminateProcess(process.Process, 1)
			windows.CloseHandle(process.Process)
		}
	}()
	if err := windows.AssignProcessToJobObject(g.job, process.Process); err != nil {
		return nil, fmt.Errorf("assign observer to Job Object: %w", err)
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		return nil, fmt.Errorf("resume observer process: %w", err)
	}
	stdinRead.Close()
	stdoutWrite.Close()
	stderrWrite.Close()
	return &Child{
		Stdin:   stdinWrite,
		Stdout:  stdoutRead,
		Stderr:  stderrRead,
		process: process.Process,
		pid:     process.ProcessId,
	}, nil
}

func (c *Child) PID() uint32 {
	return c.pid
}

func (c *Child) Wait() (uint32, error) {
	status, err := windows.WaitForSingleObject(c.process, windows.INFINITE)
	if err != nil {
		return 0, err
	}
	if status != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("unexpected wait status %d", status)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(c.process, &exitCode); err != nil {
		return 0, err
	}
	windows.CloseHandle(c.process)
	return exitCode, nil
}

func (g *Group) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	return windows.CloseHandle(g.job)
}
