//go:build windows

package foreground

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const maxWindowsTextLength = 32768

var (
	modUser32          = windows.NewLazySystemDLL("user32.dll")
	modKernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procGetWindowTextW = modUser32.NewProc("GetWindowTextW")
	procSetLastError   = modKernel32.NewProc("SetLastError")
)

func Snapshot() (Info, error) {
	window := windows.GetForegroundWindow()
	if window == 0 {
		return Info{}, errors.New("Windows did not return a foreground window")
	}
	observedAt := time.Now().UTC()

	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(window, &processID); err != nil {
		return Info{}, fmt.Errorf("query foreground window process ID: %w", err)
	}
	if processID == 0 {
		return Info{}, errors.New("foreground window returned an invalid process ID")
	}

	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return Info{}, fmt.Errorf("open foreground process %d for limited query: %w", processID, err)
	}
	defer windows.CloseHandle(process)

	executablePath, err := queryExecutablePath(process)
	if err != nil {
		return Info{}, fmt.Errorf("query foreground process %d executable path: %w", processID, err)
	}
	windowTitle, err := queryWindowTitle(window)
	if err != nil {
		return Info{}, fmt.Errorf("query foreground process %d window title: %w", processID, err)
	}

	info := Info{
		ObservedAt:     observedAt,
		ProcessID:      processID,
		ExecutableName: executableName(executablePath),
		ExecutablePath: executablePath,
		WindowTitle:    windowTitle,
	}
	if err := info.Validate(); err != nil {
		return Info{}, err
	}
	return info, nil
}

func queryExecutablePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, maxWindowsTextLength)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	if size == 0 || size >= uint32(len(buffer)) {
		return "", errors.New("QueryFullProcessImageNameW returned an invalid path length")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func queryWindowTitle(window windows.HWND) (string, error) {
	buffer := make([]uint16, maxWindowsTextLength)
	procSetLastError.Call(0)
	length, _, callErr := procGetWindowTextW.Call(
		uintptr(window),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if length == 0 && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return "", callErr
	}
	if length >= uintptr(len(buffer)) {
		return "", errors.New("GetWindowTextW returned an invalid title length")
	}
	return windows.UTF16ToString(buffer[:length]), nil
}
