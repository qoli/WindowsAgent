//go:build windows && amd64

package wgc

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/whiteboxsolutions/go-ole"
	winapirt "github.com/whiteboxsolutions/winapi/winrt"
)

const (
	graphicsCaptureAccessClass = "Windows.Graphics.Capture.GraphicsCaptureAccess"
	borderlessAccessKind       = int32(0)
	asyncStatusStarted         = int32(0)
	asyncStatusCompleted       = int32(1)
	asyncStatusCanceled        = int32(2)
	asyncStatusError           = int32(3)
	borderlessPromptTimeout    = 2 * time.Minute
)

var (
	iidGraphicsCaptureAccessStatics = ole.NewGUID("{743ED370-06EC-5040-A58A-901F0F757095}")
	iidAsyncInfo                    = ole.NewGUID("{00000036-0000-0000-C000-000000000046}")
)

func requestBorderlessCapture(ctx context.Context) error {
	factory, err := ole.RoGetActivationFactory(graphicsCaptureAccessClass, iidGraphicsCaptureAccessStatics)
	if err != nil {
		return fmt.Errorf("activate GraphicsCaptureAccess: %w", err)
	}
	defer factory.Release()

	var operation unsafe.Pointer
	if err := callHRESULTWith(
		factoryPointer(factory),
		6,
		uintptr(borderlessAccessKind),
		uintptr(unsafe.Pointer(&operation)),
	); err != nil {
		return fmt.Errorf("request borderless capture access: %w", err)
	}
	if operation == nil {
		return errors.New("borderless capture access returned a nil async operation")
	}
	defer release(operation)

	var asyncInfo unsafe.Pointer
	if err := queryInterface(operation, iidAsyncInfo, &asyncInfo); err != nil {
		return fmt.Errorf("query borderless capture async status: %w", err)
	}
	defer release(asyncInfo)

	waitContext, cancel := context.WithTimeout(ctx, borderlessPromptTimeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status int32
		if err := callHRESULTWith(asyncInfo, 7, uintptr(unsafe.Pointer(&status))); err != nil {
			return fmt.Errorf("read borderless capture access status: %w", err)
		}
		switch status {
		case asyncStatusCompleted:
			var accessStatus int32
			if err := callHRESULTWith(operation, 8, uintptr(unsafe.Pointer(&accessStatus))); err != nil {
				return fmt.Errorf("resolve borderless capture access: %w", err)
			}
			return requireBorderlessAccess(accessStatus)
		case asyncStatusCanceled:
			return errors.New("borderless capture access request was canceled")
		case asyncStatusError:
			var asyncHRESULT int32
			if err := callHRESULTWith(asyncInfo, 8, uintptr(unsafe.Pointer(&asyncHRESULT))); err != nil {
				return fmt.Errorf("read borderless capture access failure: %w", err)
			}
			return fmt.Errorf("borderless capture access failed: HRESULT %#08x", uint32(asyncHRESULT))
		case asyncStatusStarted:
		default:
			return fmt.Errorf("borderless capture access returned unknown async status %d", status)
		}
		select {
		case <-waitContext.Done():
			return fmt.Errorf("wait for borderless capture access: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func setBorderRequired(session unsafe.Pointer, required bool) error {
	var session3 unsafe.Pointer
	if err := queryInterface(session, winapirt.IGraphicsCaptureSession3ID, &session3); err != nil {
		return fmt.Errorf("query IGraphicsCaptureSession3: %w", err)
	}
	defer release(session3)
	value := uintptr(0)
	if required {
		value = 1
	}
	if err := callHRESULTWith(session3, 7, value); err != nil {
		return fmt.Errorf("set IsBorderRequired=%t: %w", required, err)
	}
	var actual uint8
	if err := callHRESULTWith(session3, 6, uintptr(unsafe.Pointer(&actual))); err != nil {
		return fmt.Errorf("read IsBorderRequired: %w", err)
	}
	return requireBorderSetting(required, actual != 0)
}
