//go:build windows

package assistbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func startApplyHandoff(operation, stage, catalogPath, dataDir string, startAtSignIn bool, clientPID, backendPID int) error {
	helper := filepath.Join(stage, "windows-assist-backend.exe")
	if info, err := os.Stat(helper); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("staged Assist backend is missing: %s", helper)
	}
	arguments := []string{"--assist-apply", operation, stage, catalogPath, dataDir, strconv.FormatBool(startAtSignIn), strconv.Itoa(clientPID), strconv.Itoa(backendPID)}
	return shellExecuteElevated(helper, stage, arguments)
}

func startUninstallHandoff(dataDir string, clientPID, backendPID int) error {
	helper, err := stageUninstallHelper(dataDir)
	if err != nil {
		return err
	}
	arguments := []string{"--assist-uninstall", dataDir, strconv.Itoa(clientPID), strconv.Itoa(backendPID)}
	return shellExecuteElevated(helper, filepath.Dir(helper), arguments)
}

func startConfigureWatchdogHandoff(dataDir string, startAtSignIn bool) error {
	helper, err := stageCurrentHelper(dataDir, "configure-watchdog")
	if err != nil {
		return err
	}
	arguments := []string{"--assist-configure-watchdog", dataDir, strconv.FormatBool(startAtSignIn)}
	return shellExecuteElevated(helper, filepath.Dir(helper), arguments)
}

func shellExecuteElevated(executable, directory string, arguments []string) error {
	escaped := make([]string, len(arguments))
	for index, argument := range arguments {
		escaped[index] = syscall.EscapeArg(argument)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(executable)
	parameters, _ := windows.UTF16PtrFromString(strings.Join(escaped, " "))
	cwd, _ := windows.UTF16PtrFromString(directory)
	if err := windows.ShellExecute(0, verb, file, parameters, cwd, win.SW_HIDE); err != nil {
		return fmt.Errorf("start elevated Assist backend: %w", err)
	}
	return nil
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := windows.WaitForSingleObject(handle, 0)
		if err != nil {
			return fmt.Errorf("wait for process %d: %w", pid, err)
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("process %d did not exit within %s", pid, timeout)
		case <-ticker.C:
		}
	}
}

func ShowHelperCompletion(args []string, helperErr error) {
	operation := "WindowsAgent operation"
	if len(args) > 0 {
		switch args[0] {
		case "--assist-apply":
			if len(args) > 1 {
				operation = "WindowsAgent " + args[1]
			}
		case "--assist-uninstall":
			operation = "WindowsAgent uninstall"
		case "--assist-configure-watchdog":
			operation = "Watchdog configuration"
		}
	}
	title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
	message := operation + " completed successfully."
	flags := uint32(win.MB_OK | win.MB_ICONINFORMATION)
	if helperErr != nil {
		message = operation + " failed:\r\n\r\n" + helperErr.Error()
		flags = win.MB_OK | win.MB_ICONERROR
	}
	text, _ := windows.UTF16PtrFromString(message)
	win.MessageBox(0, text, title, flags)
}
