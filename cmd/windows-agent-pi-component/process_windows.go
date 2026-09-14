//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func runOwnedComponent(cfg componentConfig) error {
	entrypoint := filepath.Join(cfg.runtimeDir, "node_modules", "@jmfederico", "pi-web", "dist", "server", "index.js")
	if cfg.component == "sessiond" {
		entrypoint = filepath.Join(cfg.runtimeDir, "node_modules", "@jmfederico", "pi-web", "dist", "server", "sessiond.js")
	}
	for label, path := range map[string]string{"Node executable": cfg.nodePath, "PI WEB entrypoint": entrypoint, "PI WEB config": cfg.piWebConfig, "computer-use helper": cfg.helperPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file: %s", label, path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.logFile), 0o700); err != nil {
		return fmt.Errorf("create component log directory: %w", err)
	}
	logFile, err := os.OpenFile(cfg.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open component log: %w", err)
	}
	defer logFile.Close()

	sessiondPort, _ := loopbackPort(cfg.sessiondListen)
	webPort, _ := loopbackPort(cfg.webListen)
	command := exec.Command(cfg.nodePath, entrypoint)
	command.Stdout, command.Stderr = logFile, logFile
	command.Env = append(os.Environ(),
		"PI_CODING_AGENT_DIR="+cfg.agentDir,
		"PI_CODING_AGENT_SESSION_DIR="+cfg.sessionDir,
		"PI_WEB_DATA_DIR="+cfg.piWebDataDir,
		"PI_WEB_CONFIG="+cfg.piWebConfig,
		"PI_COMPUTER_USE_WINDOWS_HELPER_PATH="+cfg.helperPath,
		"PI_WEB_SESSIOND_HOST=127.0.0.1",
		"PI_WEB_SESSIOND_PORT="+sessiondPort,
		"PI_WEB_SESSIOND_URL=http://127.0.0.1:"+sessiondPort,
		"PI_WEB_HOST=127.0.0.1",
		"PI_WEB_PORT="+webPort,
	)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create component process job: %w", err)
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fmt.Errorf("configure component process job: %w", err)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start PI WEB %s: %w", cfg.component, err)
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		_ = command.Process.Kill()
		return fmt.Errorf("open PI WEB %s process for job assignment: %w", cfg.component, err)
	}
	defer windows.CloseHandle(processHandle)
	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = command.Process.Kill()
		return fmt.Errorf("assign PI WEB %s to process job: %w", cfg.component, err)
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("PI WEB %s exited: %w", cfg.component, err)
	}
	return nil
}
