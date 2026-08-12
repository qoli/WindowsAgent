//go:build windows

package wgcworker

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// StartParentLifetimeGuard binds this private worker generation to the Agent
// process that launched it. The waiter remains able to terminate this process
// even when the WGC runtime thread is blocked inside a native call.
func StartParentLifetimeGuard(parentPID int) error {
	if parentPID <= 0 || parentPID == int(windows.GetCurrentProcessId()) {
		return errors.New("WGC worker parent process ID is invalid")
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(parentPID))
	if err != nil {
		return fmt.Errorf("open WGC worker parent process %d: %w", parentPID, err)
	}
	go func() {
		defer windows.CloseHandle(parent)
		state, waitErr := windows.WaitForSingleObject(parent, windows.INFINITE)
		if waitErr == nil && state == windows.WAIT_OBJECT_0 {
			_ = windows.TerminateProcess(windows.CurrentProcess(), 1)
		}
	}()
	return nil
}
