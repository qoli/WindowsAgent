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
	inputMouse            = 0
	inputKeyboard         = 1
	mouseEventLeftDown    = 0x0002
	mouseEventLeftUp      = 0x0004
	keyEventExtendedKey   = 0x0001
	keyEventKeyUp         = 0x0002
	keyEventScanCode      = 0x0008
	mapVirtualKeyToScanEx = 4
	smCXScreen            = 0
	smCYScreen            = 1
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	sendInputProc        = user32DLL.NewProc("SendInput")
	mapVirtualKeyProc    = user32DLL.NewProc("MapVirtualKeyW")
	setCursorPosProc     = user32DLL.NewProc("SetCursorPos")
	getCursorPosProc     = user32DLL.NewProc("GetCursorPos")
	getSystemMetricsProc = user32DLL.NewProc("GetSystemMetrics")
)

type WindowsDriver struct{}

type screenPoint struct {
	X int32
	Y int32
}

func (WindowsDriver) ClickCurrent(ctx context.Context, request CurrentPointerClickRequest) (PointerEvidence, error) {
	if ctx == nil {
		return PointerEvidence{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return PointerEvidence{}, err
	}
	if request.Hold <= 0 || request.Hold > 2*time.Second {
		return PointerEvidence{}, errors.New("pointer hold duration must be between 1ms and 2s")
	}
	width, _, _ := getSystemMetricsProc.Call(smCXScreen)
	height, _, _ := getSystemMetricsProc.Call(smCYScreen)
	if width == 0 || height == 0 {
		return PointerEvidence{}, errors.New("GetSystemMetrics returned an empty primary display")
	}
	var point screenPoint
	read, _, readErr := getCursorPosProc.Call(uintptr(unsafe.Pointer(&point)))
	if read == 0 {
		if readErr != nil && readErr != syscall.Errno(0) {
			return PointerEvidence{}, fmt.Errorf("GetCursorPos failed: %w", readErr)
		}
		return PointerEvidence{}, errors.New("GetCursorPos failed")
	}
	if point.X < 0 || point.Y < 0 || point.X >= int32(width) || point.Y >= int32(height) {
		return PointerEvidence{}, fmt.Errorf("current pointer position (%d,%d) is outside the primary display %dx%d", point.X, point.Y, width, height)
	}
	if err := clickLeft(ctx, request.Hold); err != nil {
		return PointerEvidence{}, err
	}
	return PointerEvidence{Backend: BackendSendInputPointer, ScreenX: int(point.X), ScreenY: int(point.Y), ScreenWidth: int(width), ScreenHeight: int(height), HoldMS: request.Hold.Milliseconds()}, nil
}

func (WindowsDriver) ClickReference(ctx context.Context, request PointerClickRequest) (PointerEvidence, error) {
	if ctx == nil {
		return PointerEvidence{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return PointerEvidence{}, err
	}
	if request.ReferenceX < 0 || request.ReferenceX >= 1920 || request.ReferenceY < 0 || request.ReferenceY >= 1080 {
		return PointerEvidence{}, errors.New("reference pointer coordinates must be inside 1920x1080")
	}
	if request.Hold <= 0 || request.Hold > 2*time.Second {
		return PointerEvidence{}, errors.New("pointer hold duration must be between 1ms and 2s")
	}
	width, _, _ := getSystemMetricsProc.Call(smCXScreen)
	height, _, _ := getSystemMetricsProc.Call(smCYScreen)
	if width == 0 || height == 0 {
		return PointerEvidence{}, errors.New("GetSystemMetrics returned an empty primary display")
	}
	screenWidth, screenHeight := int(width), int(height)
	viewportWidth, viewportHeight := screenWidth, screenHeight
	if screenWidth*9 > screenHeight*16 {
		viewportWidth = screenHeight * 16 / 9
	} else if screenWidth*9 < screenHeight*16 {
		viewportHeight = screenWidth * 9 / 16
	}
	viewportX := (screenWidth - viewportWidth) / 2
	viewportY := (screenHeight - viewportHeight) / 2
	screenX := viewportX + request.ReferenceX*viewportWidth/1920
	screenY := viewportY + request.ReferenceY*viewportHeight/1080
	moved, _, moveErr := setCursorPosProc.Call(uintptr(screenX), uintptr(screenY))
	if moved == 0 {
		if moveErr != nil && moveErr != syscall.Errno(0) {
			return PointerEvidence{}, fmt.Errorf("SetCursorPos failed: %w", moveErr)
		}
		return PointerEvidence{}, errors.New("SetCursorPos failed")
	}
	if err := clickLeft(ctx, request.Hold); err != nil {
		return PointerEvidence{}, err
	}
	return PointerEvidence{Backend: BackendSendInputPointer, ReferenceX: request.ReferenceX, ReferenceY: request.ReferenceY,
		ScreenX: screenX, ScreenY: screenY, ScreenWidth: screenWidth, ScreenHeight: screenHeight,
		ViewportX: viewportX, ViewportY: viewportY, ViewportWidth: viewportWidth, ViewportHeight: viewportHeight,
		HoldMS: request.Hold.Milliseconds()}, nil
}

func clickLeft(ctx context.Context, hold time.Duration) error {
	if err := sendMouseButton(mouseEventLeftDown); err != nil {
		return fmt.Errorf("send pointer left down: %w", err)
	}
	timer := time.NewTimer(hold)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
	}
	releaseErr := sendMouseButton(mouseEventLeftUp)
	if releaseErr != nil {
		releaseErr = fmt.Errorf("send pointer left up: %w", releaseErr)
	}
	return errors.Join(ctx.Err(), releaseErr)
}

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
	evidence, flags, err := resolveKey(request.Key)
	if err != nil {
		return Evidence{}, err
	}
	evidence.HoldMS = request.Hold.Milliseconds()
	if err := sendKeyboardScanCode(evidence.ScanCode, flags); err != nil {
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
	releaseErr := sendKeyboardScanCode(evidence.ScanCode, flags|keyEventKeyUp)
	if releaseErr != nil {
		releaseErr = fmt.Errorf("send scan-code key up: %w", releaseErr)
	}
	if err := errors.Join(waitErr, releaseErr); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func (WindowsDriver) KeyDown(ctx context.Context, request KeyRequest) (Evidence, error) {
	if ctx == nil {
		return Evidence{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
	evidence, flags, err := resolveKey(request.Key)
	if err != nil {
		return Evidence{}, err
	}
	if err := sendKeyboardScanCode(evidence.ScanCode, flags); err != nil {
		return Evidence{}, fmt.Errorf("send scan-code key down: %w", err)
	}
	return evidence, nil
}

func (WindowsDriver) KeyUp(ctx context.Context, request KeyRequest) (Evidence, error) {
	if ctx == nil {
		return Evidence{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
	evidence, flags, err := resolveKey(request.Key)
	if err != nil {
		return Evidence{}, err
	}
	if err := sendKeyboardScanCode(evidence.ScanCode, flags|keyEventKeyUp); err != nil {
		return Evidence{}, fmt.Errorf("send scan-code key up: %w", err)
	}
	return evidence, nil
}

func resolveKey(key string) (Evidence, uint32, error) {
	virtualKey, err := VirtualKey(key)
	if err != nil {
		return Evidence{}, 0, err
	}
	mapped, _, mapErr := mapVirtualKeyProc.Call(uintptr(virtualKey), mapVirtualKeyToScanEx)
	if mapped == 0 {
		if mapErr != nil && mapErr != syscall.Errno(0) {
			return Evidence{}, 0, fmt.Errorf("MapVirtualKeyW failed for %s: %w", key, mapErr)
		}
		return Evidence{}, 0, fmt.Errorf("MapVirtualKeyW returned no scan code for %s", key)
	}
	scanCode, extended, err := decodeMappedScanCode(mapped)
	if err != nil {
		return Evidence{}, 0, fmt.Errorf("map key %s to scan code: %w", key, err)
	}
	if RequiresExtendedScanCode(key) {
		extended = true
	}
	flags := uint32(keyEventScanCode)
	if extended {
		flags |= keyEventExtendedKey
	}
	return Evidence{
		Backend: BackendSendInputScanCode, Key: key, ScanCode: scanCode, Extended: extended,
	}, flags, nil
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

type windowsMouseInput struct {
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

func sendMouseButton(flags uint32) error {
	input := windowsInput{Type: inputMouse}
	mouse := (*windowsMouseInput)(unsafe.Pointer(&input.Data[0]))
	mouse.Flags = flags
	inserted, _, callErr := sendInputProc.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if inserted == 1 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("SendInput inserted %d of 1 record: %w", inserted, callErr)
	}
	return fmt.Errorf("SendInput inserted %d of 1 record", inserted)
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
