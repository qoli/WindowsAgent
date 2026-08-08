//go:build windows

package windowsinput

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	inputKeyboard         = 1
	keyEventExtendedKey   = 0x0001
	keyEventKeyUp         = 0x0002
	keyEventScanCode      = 0x0008
	mapVirtualKeyToScanEx = 4
)

var (
	user32DLL         = syscall.NewLazyDLL("user32.dll")
	sendInputProc     = user32DLL.NewProc("SendInput")
	mapVirtualKeyProc = user32DLL.NewProc("MapVirtualKeyW")
)

type WindowsDriver struct{}

func (WindowsDriver) Press(ctx context.Context, request PressRequest) (Evidence, error) {
	if ctx == nil {
		return Evidence{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
	if request.Hold <= 0 || request.Hold > time.Second {
		return Evidence{}, errors.New("key press hold duration must be between 1ms and 1s")
	}
	virtualKey, err := VirtualKey(request.Key)
	if err != nil {
		return Evidence{}, err
	}
	mapped, _, mapErr := mapVirtualKeyProc.Call(uintptr(virtualKey), mapVirtualKeyToScanEx)
	if mapped == 0 {
		if mapErr != nil && mapErr != syscall.Errno(0) {
			return Evidence{}, fmt.Errorf("MapVirtualKeyW failed for %s: %w", request.Key, mapErr)
		}
		return Evidence{}, fmt.Errorf("MapVirtualKeyW returned no scan code for %s", request.Key)
	}
	scanCode, extended, err := decodeMappedScanCode(mapped)
	if err != nil {
		return Evidence{}, fmt.Errorf("map key %s to scan code: %w", request.Key, err)
	}
	if RequiresExtendedScanCode(request.Key) {
		extended = true
	}
	flags := uint32(keyEventScanCode)
	if extended {
		flags |= keyEventExtendedKey
	}
	evidence := Evidence{
		Backend: BackendSendInputScanCode, Key: request.Key, ScanCode: scanCode,
		Extended: extended, HoldMS: request.Hold.Milliseconds(),
	}
	if err := sendKeyboardScanCode(scanCode, flags); err != nil {
		return Evidence{}, fmt.Errorf("send scan-code key down: %w", err)
	}

	timer := time.NewTimer(request.Hold)
	var waitErr error
	select {
	case <-ctx.Done():
		waitErr = ctx.Err()
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}
	releaseErr := sendKeyboardScanCode(scanCode, flags|keyEventKeyUp)
	if releaseErr != nil {
		releaseErr = fmt.Errorf("send scan-code key up: %w", releaseErr)
	}
	if err := errors.Join(waitErr, releaseErr); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

type windowsInput struct {
	Type uint32
	_    uint32
	Data [32]byte
}

type windowsKeyboardInput struct {
	VirtualKey uint16
	ScanCode   uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
}

func sendKeyboardScanCode(scanCode uint16, flags uint32) error {
	input := keyboardInputFor(scanCode, flags)
	if unsafe.Sizeof(input) != 40 {
		return fmt.Errorf("unexpected Windows INPUT size %d", unsafe.Sizeof(input))
	}
	inserted, _, callErr := sendInputProc.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if inserted == 1 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("SendInput inserted %d of 1 record: %w", inserted, callErr)
	}
	return fmt.Errorf("SendInput inserted %d of 1 record", inserted)
}

func keyboardInputFor(scanCode uint16, flags uint32) windowsInput {
	result := windowsInput{Type: inputKeyboard}
	keyboard := (*windowsKeyboardInput)(unsafe.Pointer(&result.Data[0]))
	keyboard.ScanCode = scanCode
	keyboard.Flags = flags
	return result
}
