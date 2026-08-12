//go:build windows

package recordingindicator

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

// Publisher keeps the manual-reset event alive only while its owning Evidence
// stream is recording. Multiple recorders may hold the same event concurrently.
type Publisher struct {
	mu     sync.Mutex
	handle windows.Handle
}

func NewPublisher() *Publisher { return &Publisher{} }

func (p *Publisher) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle != 0 {
		return errors.New("Evidence recording indicator is already active")
	}
	name, err := windows.UTF16PtrFromString(SignalName)
	if err != nil {
		return fmt.Errorf("encode Evidence recording indicator name: %w", err)
	}
	handle, err := windows.CreateEvent(nil, 1, 1, name)
	if err != nil {
		return fmt.Errorf("create Evidence recording indicator: %w", err)
	}
	p.handle = handle
	return nil
}

func (p *Publisher) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == 0 {
		return
	}
	_ = windows.CloseHandle(p.handle)
	p.handle = 0
}

// Active opens and closes a fresh observer handle on every call. Retaining an
// observer handle would keep the kernel object alive after the recorder exits
// and would turn a stopped recorder into a false positive.
func Active() (bool, error) {
	name, err := windows.UTF16PtrFromString(SignalName)
	if err != nil {
		return false, fmt.Errorf("encode Evidence recording indicator name: %w", err)
	}
	handle, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open Evidence recording indicator: %w", err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, fmt.Errorf("read Evidence recording indicator: %w", err)
	}
	if status != windows.WAIT_OBJECT_0 {
		return false, fmt.Errorf("Evidence recording indicator is unexpectedly unsignaled: wait status %#x", status)
	}
	return true, nil
}
