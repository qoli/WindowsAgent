//go:build windows

package watchdog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type WindowsProcessInspector struct{}

func (WindowsProcessInspector) FindByExecutablePath(ctx context.Context, expectedPath string) ([]ProcessInfo, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("create process snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafeSizeofProcessEntry32())}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("read first process snapshot entry: %w", err)
	}
	wanted := filepath.Clean(expectedPath)
	wantedBase := filepath.Base(wanted)
	var matches []ProcessInfo
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entryName := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(entryName, wantedBase) {
			path, pathErr := processImagePath(entry.ProcessID)
			if pathErr != nil {
				return nil, fmt.Errorf("resolve path for candidate process %d: %w", entry.ProcessID, pathErr)
			}
			if !strings.EqualFold(filepath.Clean(path), wanted) {
				goto next
			}
			var sessionID uint32
			if err := windows.ProcessIdToSessionId(entry.ProcessID, &sessionID); err != nil {
				return nil, fmt.Errorf("resolve session for process %d: %w", entry.ProcessID, err)
			}
			matches = append(matches, ProcessInfo{PID: entry.ProcessID, SessionID: sessionID})
		}
	next:
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("read next process snapshot entry: %w", err)
		}
	}
	return matches, nil
}

func processImagePath(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	if size == 0 || size > uint32(len(buffer)) {
		return "", errors.New("QueryFullProcessImageNameW returned an invalid path length")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func unsafeSizeofProcessEntry32() uintptr {
	var entry windows.ProcessEntry32
	return unsafe.Sizeof(entry)
}
