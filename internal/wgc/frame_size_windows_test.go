//go:build windows && amd64

package wgc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	winapirt "github.com/whiteboxsolutions/winapi/winrt"
)

// This opt-in integration test changes only capture buffers, never the display
// mode or foreground window. It requires the signed-in interactive session.
func TestWGCFramePoolResizeLive(t *testing.T) {
	if os.Getenv("WINDOWS_AGENT_WGC_RESIZE_TEST") != "1" {
		t.Skip("set WINDOWS_AGENT_WGC_RESIZE_TEST=1 in the interactive Windows session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := onWinRTThread(ctx, func() (struct{}, error) {
		target, err := findPrimaryDisplay()
		if err != nil {
			return struct{}{}, err
		}
		defer release(target.adapter)
		format := uint32(dxgiFormatB8G8R8A8UNorm)
		if target.desc.ColorSpace == dxgiColorSpaceRGBFullG2084NoneP2020 {
			format = dxgiFormatR16G16B16A16Float
		}
		device, context3D, winRTDevice, err := createD3DDevice(target.adapter)
		if err != nil {
			return struct{}{}, err
		}
		defer release(device)
		defer release(context3D)
		defer release(winRTDevice)
		item, native, err := createMonitorItem(target.desc.Monitor)
		if err != nil {
			return struct{}{}, err
		}
		defer release(item)
		if err := requestBorderlessCapture(ctx); err != nil {
			return struct{}{}, err
		}
		for _, initial := range []winapirt.SizeInt32{
			{Width: native.Width / 2, Height: native.Height / 2},
			{Width: native.Width + native.Width/2, Height: native.Height + native.Height/2},
		} {
			err := func() error {
				pool, err := createFreeThreadedFramePoolWithBuffers(winRTDevice, int32(format), initial, 2)
				if err != nil {
					return err
				}
				defer closeAndRelease(pool)
				session, err := createCaptureSession(pool, item)
				if err != nil {
					return err
				}
				defer closeAndRelease(session)
				if err := setCursorCapture(session, false); err != nil {
					return err
				}
				if err := setBorderRequired(session, false); err != nil {
					return err
				}
				if err := callHRESULT(session, 6); err != nil {
					return err
				}
				size := initial
				resizes := 0
				frame, err := latestSizedFrame(ctx, pool, winRTDevice, format, 2, &size,
					func(before, after winapirt.SizeInt32) {
						resizes++
						t.Logf("pool %dx%d -> content %dx%d", before.Width, before.Height, after.Width, after.Height)
					})
				if err != nil {
					return err
				}
				defer closeAndRelease(frame)
				texture, desc, err := openFrameTexture(frame, format)
				if err != nil {
					return err
				}
				defer release(texture)
				if resizes == 0 || size != native || int32(desc.Width) != native.Width || int32(desc.Height) != native.Height {
					return fmt.Errorf("initial=%v final=%v native=%v texture=%dx%d resizes=%d", initial, size, native, desc.Width, desc.Height, resizes)
				}
				return nil
			}()
			if err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
