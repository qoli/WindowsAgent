//go:build windows

package inputaction

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	inputKeyboard = 1
	keyEventKeyUp = 0x0002
)

var sendInputProc = syscall.NewLazyDLL("user32.dll").NewProc("SendInput")

type WindowsSender struct{}

func (WindowsSender) Press(ctx context.Context, virtualKey uint16) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inputs := [2]windowsInput{keyboardInputFor(virtualKey, 0), keyboardInputFor(virtualKey, keyEventKeyUp)}
	if unsafe.Sizeof(inputs[0]) != 40 {
		return fmt.Errorf("unexpected Windows INPUT size %d", unsafe.Sizeof(inputs[0]))
	}
	inserted, _, callErr := sendInputProc.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if inserted == uintptr(len(inputs)) {
		return nil
	}
	// If only key-down was accepted, release the key before reporting failure.
	if inserted == 1 {
		release := keyboardInputFor(virtualKey, keyEventKeyUp)
		_, _, _ = sendInputProc.Call(1, uintptr(unsafe.Pointer(&release)), unsafe.Sizeof(release))
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("SendInput inserted %d of %d records: %w", inserted, len(inputs), callErr)
	}
	return fmt.Errorf("SendInput inserted %d of %d records", inserted, len(inputs))
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

func keyboardInputFor(virtualKey uint16, flags uint32) windowsInput {
	result := windowsInput{Type: inputKeyboard}
	keyboard := (*windowsKeyboardInput)(unsafe.Pointer(&result.Data[0]))
	keyboard.VirtualKey = virtualKey
	keyboard.Flags = flags
	return result
}
