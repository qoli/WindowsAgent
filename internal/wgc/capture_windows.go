//go:build windows && amd64

package wgc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/pixels"
	"github.com/whiteboxsolutions/go-ole"
	winapirt "github.com/whiteboxsolutions/winapi/winrt"
	"golang.org/x/sys/windows"
)

const (
	dxgiErrorNotFound = 0x887a0002

	dxgiColorSpaceRGBFullG22NoneP709    = 0
	dxgiColorSpaceRGBFullG10NoneP709    = 1
	dxgiColorSpaceRGBFullG2084NoneP2020 = 12

	d3dDriverTypeUnknown         = 0
	d3d11CreateDeviceBGRASupport = 0x20
	d3d11SDKVersion              = 7
	d3d11UsageStaging            = 3
	d3d11CPUAccessRead           = 0x20000

	dxgiFormatR16G16B16A16Float = 10
	dxgiFormatB8G8R8A8UNorm     = 87

	monitorDefaultToPrimary = 2
)

var (
	modUser32  = windows.NewLazySystemDLL("user32.dll")
	modCombase = windows.NewLazySystemDLL("combase.dll")
	modDXGI    = windows.NewLazySystemDLL("dxgi.dll")
	modD3D11   = windows.NewLazySystemDLL("d3d11.dll")

	procSetProcessDPIAwarenessContext        = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procMonitorFromPoint                     = modUser32.NewProc("MonitorFromPoint")
	procRoInitialize                         = modCombase.NewProc("RoInitialize")
	procRoUninitialize                       = modCombase.NewProc("RoUninitialize")
	procCreateDXGIFactory1                   = modDXGI.NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice                    = modD3D11.NewProc("D3D11CreateDevice")
	procCreateDirect3D11DeviceFromDXGIDevice = modD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")

	iidIDXGIFactory1 = ole.NewGUID("{770aae78-f26f-4dba-a829-253c83d1b387}")
	iidIDXGIOutput6  = ole.NewGUID("{068346e8-aaec-4b84-add7-137f513f77a1}")
	iidIDXGIDevice   = ole.NewGUID("{54ec77fa-1377-44e6-8c32-88fd5f44c84c}")
	iidTexture2D     = ole.NewGUID("{6f15aaf2-d208-4e89-9ab4-489535d34f9c}")
	iidDXGIInterface = ole.NewGUID("{a9b3d012-3df2-4ee3-b8d1-8695f457d3c1}")
	iidClosable      = ole.NewGUID("{30d5a829-7fa4-4026-83bb-d75bae4ea99e}")
)

type Capturer struct {
	logger *slog.Logger
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type outputDesc1 struct {
	DeviceName            [32]uint16
	DesktopCoordinates    rect
	AttachedToDesktop     int32
	Rotation              uint32
	Monitor               uintptr
	BitsPerColor          uint32
	ColorSpace            int32
	RedPrimary            [2]float32
	GreenPrimary          [2]float32
	BluePrimary           [2]float32
	WhitePoint            [2]float32
	MinLuminance          float32
	MaxLuminance          float32
	MaxFullFrameLuminance float32
}

type displayTarget struct {
	adapter unsafe.Pointer
	desc    outputDesc1
}

type sampleDesc struct {
	Count   uint32
	Quality uint32
}

type texture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     sampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type mappedSubresource struct {
	Data       unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

func New(logger *slog.Logger) (*Capturer, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	dpiContextPerMonitorAwareV2 := ^uintptr(3)
	ok, _, callErr := procSetProcessDPIAwarenessContext.Call(dpiContextPerMonitorAwareV2)
	if ok == 0 {
		return nil, fmt.Errorf("set per-monitor-v2 DPI awareness: %w", callErr)
	}
	return &Capturer{logger: logger}, nil
}

func (c *Capturer) Status(ctx context.Context) (capture.Status, error) {
	return onWinRTThread(ctx, func() (capture.Status, error) {
		supported, err := graphicsCaptureSupported()
		if err != nil {
			return capture.Status{}, capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		}
		if !supported {
			return capture.Status{Supported: false}, nil
		}
		target, err := findPrimaryDisplay()
		if err != nil {
			return capture.Status{}, err
		}
		defer release(target.adapter)
		return capture.Status{
			Supported: true,
			Monitor:   monitorFromDesc(target.desc),
		}, nil
	})
}

func (c *Capturer) Capture(ctx context.Context, includeCursor bool) (capture.Result, error) {
	return onWinRTThread(ctx, func() (capture.Result, error) {
		supported, err := graphicsCaptureSupported()
		if err != nil {
			return capture.Result{}, capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		}
		if !supported {
			return capture.Result{}, capture.Failure("capture_unsupported", "Windows Graphics Capture is not supported", nil)
		}

		target, err := findPrimaryDisplay()
		if err != nil {
			return capture.Result{}, err
		}
		defer release(target.adapter)
		monitor := monitorFromDesc(target.desc)

		pixelFormat := uint32(dxgiFormatB8G8R8A8UNorm)
		pixelFormatName := "B8G8R8A8_UNORM"
		switch target.desc.ColorSpace {
		case dxgiColorSpaceRGBFullG22NoneP709:
		case dxgiColorSpaceRGBFullG2084NoneP2020:
			if !finitePositiveAbove80(float64(target.desc.MaxLuminance)) {
				return capture.Result{}, capture.Failure(
					"invalid_hdr_metadata",
					"HDR display metadata must provide finite maximum luminance above 80 nits",
					nil,
				)
			}
			pixelFormat = dxgiFormatR16G16B16A16Float
			pixelFormatName = "R16G16B16A16_FLOAT"
		default:
			return capture.Result{}, capture.Failure(
				"unsupported_color_space",
				fmt.Sprintf("unsupported primary-monitor color space: %s", colorSpaceName(target.desc.ColorSpace)),
				nil,
			)
		}

		device, context3D, winRTDevice, err := createD3DDevice(target.adapter)
		if err != nil {
			return capture.Result{}, capture.Failure("capture_device_failed", "failed to create the Direct3D 11 capture device", err)
		}
		defer release(device)
		defer release(context3D)
		defer release(winRTDevice)

		item, size, err := createMonitorItem(target.desc.Monitor)
		if err != nil {
			return capture.Result{}, capture.Failure("desktop_unavailable", "failed to create a capture item for the primary monitor", err)
		}
		defer release(item)
		if size.Width <= 0 || size.Height <= 0 {
			return capture.Result{}, capture.Failure("desktop_unavailable", "primary monitor capture size is invalid", nil)
		}

		framePool, err := createFreeThreadedFramePool(winRTDevice, int32(pixelFormat), size)
		if err != nil {
			return capture.Result{}, capture.Failure("capture_session_failed", "failed to create the WGC frame pool", err)
		}
		defer closeAndRelease(framePool)

		session, err := createCaptureSession(framePool, item)
		if err != nil {
			return capture.Result{}, capture.Failure("capture_session_failed", "failed to create the WGC capture session", err)
		}
		defer closeAndRelease(session)
		if err := setCursorCapture(session, includeCursor); err != nil {
			return capture.Result{}, capture.Failure("capture_session_failed", "failed to configure cursor capture", err)
		}
		if err := callHRESULT(session, 6); err != nil {
			return capture.Result{}, capture.Failure("capture_session_failed", "failed to start WGC capture", err)
		}

		frame, err := waitForFrame(ctx, framePool)
		if err != nil {
			return capture.Result{}, err
		}
		defer closeAndRelease(frame)

		image, width, height, err := readFrame(frame, device, context3D, pixelFormat, target.desc)
		if err != nil {
			return capture.Result{}, err
		}
		if width != int(size.Width) || height != int(size.Height) {
			return capture.Result{}, capture.Failure(
				"capture_size_mismatch",
				fmt.Sprintf("captured texture is %dx%d but WGC item is %dx%d", width, height, size.Width, size.Height),
				nil,
			)
		}

		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image); err != nil {
			return capture.Result{}, capture.Failure("capture_encode_failed", "failed to encode captured frame as PNG", err)
		}
		monitor.Width = width
		monitor.Height = height
		return capture.Result{
			PNG:                encoded.Bytes(),
			Width:              width,
			Height:             height,
			IncludeCursor:      includeCursor,
			Monitor:            monitor,
			CapturePixelFormat: pixelFormatName,
			ToneMapped:         monitor.HDR,
		}, nil
	})
}

func onWinRTThread[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procRoInitialize.Call(uintptr(winapirt.RO_INIT_MULTITHREADED))
	if hr != 0 && hr != 1 {
		return zero, fmt.Errorf("RoInitialize(MTA): HRESULT %#08x", uint32(hr))
	}
	defer procRoUninitialize.Call()
	return operation()
}

func graphicsCaptureSupported() (bool, error) {
	const graphicsCaptureSessionClass = "Windows.Graphics.Capture.GraphicsCaptureSession"
	factory, err := ole.RoGetActivationFactory(graphicsCaptureSessionClass, winapirt.IGraphicsCaptureSessionStaticsID)
	if err != nil {
		return false, err
	}
	defer factory.Release()
	var supported uint8
	if err := callHRESULTWith(factoryPointer(factory), 6, uintptr(unsafe.Pointer(&supported))); err != nil {
		return false, err
	}
	return supported != 0, nil
}

func findPrimaryDisplay() (displayTarget, error) {
	primary, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	if primary == 0 {
		return displayTarget{}, capture.Failure("desktop_unavailable", "Windows did not return a primary monitor", nil)
	}

	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if err := checkHRESULT(hr, "CreateDXGIFactory1"); err != nil {
		return displayTarget{}, capture.Failure("capture_device_failed", "failed to create DXGI factory", err)
	}
	defer release(factory)

	for adapterIndex := uint32(0); ; adapterIndex++ {
		var adapter unsafe.Pointer
		hr, _, _ := syscall.SyscallN(
			comMethod(factory, 12),
			uintptr(factory),
			uintptr(adapterIndex),
			uintptr(unsafe.Pointer(&adapter)),
		)
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if err := checkHRESULT(hr, "IDXGIFactory1.EnumAdapters1"); err != nil {
			return displayTarget{}, capture.Failure("capture_device_failed", "failed to enumerate DXGI adapters", err)
		}

		matched := false
		for outputIndex := uint32(0); ; outputIndex++ {
			var output unsafe.Pointer
			hr, _, _ = syscall.SyscallN(
				comMethod(adapter, 7),
				uintptr(adapter),
				uintptr(outputIndex),
				uintptr(unsafe.Pointer(&output)),
			)
			if uint32(hr) == dxgiErrorNotFound {
				break
			}
			if err := checkHRESULT(hr, "IDXGIAdapter1.EnumOutputs"); err != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "failed to enumerate DXGI outputs", err)
			}

			var output6 unsafe.Pointer
			queryErr := queryInterface(output, iidIDXGIOutput6, &output6)
			release(output)
			if queryErr != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "primary output does not expose IDXGIOutput6", queryErr)
			}
			var desc outputDesc1
			hr, _, _ = syscall.SyscallN(
				comMethod(output6, 27),
				uintptr(output6),
				uintptr(unsafe.Pointer(&desc)),
			)
			release(output6)
			if err := checkHRESULT(hr, "IDXGIOutput6.GetDesc1"); err != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "failed to read DXGI output metadata", err)
			}
			if desc.Monitor == primary {
				if desc.AttachedToDesktop == 0 {
					release(adapter)
					return displayTarget{}, capture.Failure("desktop_unavailable", "primary monitor is not attached to the desktop", nil)
				}
				matched = true
				return displayTarget{adapter: adapter, desc: desc}, nil
			}
		}
		if !matched {
			release(adapter)
		}
	}
	return displayTarget{}, capture.Failure("desktop_unavailable", "primary monitor was not found in active DXGI outputs", nil)
}

func createD3DDevice(adapter unsafe.Pointer) (unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, error) {
	var device unsafe.Pointer
	var context3D unsafe.Pointer
	hr, _, _ := procD3D11CreateDevice.Call(
		uintptr(adapter),
		d3dDriverTypeUnknown,
		0,
		d3d11CreateDeviceBGRASupport,
		0,
		0,
		d3d11SDKVersion,
		uintptr(unsafe.Pointer(&device)),
		0,
		uintptr(unsafe.Pointer(&context3D)),
	)
	if err := checkHRESULT(hr, "D3D11CreateDevice"); err != nil {
		return nil, nil, nil, err
	}

	var dxgiDevice unsafe.Pointer
	if err := queryInterface(device, iidIDXGIDevice, &dxgiDevice); err != nil {
		release(context3D)
		release(device)
		return nil, nil, nil, fmt.Errorf("query IDXGIDevice: %w", err)
	}
	defer release(dxgiDevice)

	var winRTDevice unsafe.Pointer
	hr, _, _ = procCreateDirect3D11DeviceFromDXGIDevice.Call(
		uintptr(dxgiDevice),
		uintptr(unsafe.Pointer(&winRTDevice)),
	)
	if err := checkHRESULT(hr, "CreateDirect3D11DeviceFromDXGIDevice"); err != nil {
		release(context3D)
		release(device)
		return nil, nil, nil, err
	}
	return device, context3D, winRTDevice, nil
}

func createMonitorItem(monitor uintptr) (unsafe.Pointer, winapirt.SizeInt32, error) {
	factory, err := ole.RoGetActivationFactory(winapirt.GraphicsCaptureItemClass, winapirt.IGraphicsCaptureItemInteropID)
	if err != nil {
		return nil, winapirt.SizeInt32{}, err
	}
	defer factory.Release()

	var item unsafe.Pointer
	if err := callHRESULTWith(
		factoryPointer(factory),
		4,
		monitor,
		uintptr(unsafe.Pointer(winapirt.IGraphicsCaptureItemID)),
		uintptr(unsafe.Pointer(&item)),
	); err != nil {
		return nil, winapirt.SizeInt32{}, err
	}
	var size winapirt.SizeInt32
	if err := callHRESULTWith(item, 7, uintptr(unsafe.Pointer(&size))); err != nil {
		release(item)
		return nil, winapirt.SizeInt32{}, err
	}
	return item, size, nil
}

func createFreeThreadedFramePool(device unsafe.Pointer, pixelFormat int32, size winapirt.SizeInt32) (unsafe.Pointer, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errors.New("SizeInt32 ABI wrapper requires windows/amd64")
	}
	packedSize := uint64(uint32(size.Width)) | uint64(uint32(size.Height))<<32
	if err := pixels.ValidatePackedSize(size.Width, size.Height, packedSize); err != nil {
		return nil, err
	}
	factory, err := ole.RoGetActivationFactory(winapirt.Direct3D11CaptureFramePoolClass, winapirt.IDirect3D11CaptureFramePoolStatics2ID)
	if err != nil {
		return nil, err
	}
	defer factory.Release()

	var framePool unsafe.Pointer
	if err := callHRESULTWith(
		factoryPointer(factory),
		6,
		uintptr(device),
		uintptr(pixelFormat),
		1,
		uintptr(packedSize),
		uintptr(unsafe.Pointer(&framePool)),
	); err != nil {
		return nil, err
	}
	return framePool, nil
}

func createCaptureSession(framePool, item unsafe.Pointer) (unsafe.Pointer, error) {
	var session unsafe.Pointer
	if err := callHRESULTWith(
		framePool,
		10,
		uintptr(item),
		uintptr(unsafe.Pointer(&session)),
	); err != nil {
		return nil, err
	}
	return session, nil
}

func setCursorCapture(session unsafe.Pointer, include bool) error {
	var session2 unsafe.Pointer
	if err := queryInterface(session, winapirt.IGraphicsCaptureSession2ID, &session2); err != nil {
		return err
	}
	defer release(session2)
	value := uintptr(0)
	if include {
		value = 1
	}
	return callHRESULTWith(session2, 7, value)
}

func waitForFrame(ctx context.Context, framePool unsafe.Pointer) (unsafe.Pointer, error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var frame unsafe.Pointer
		if err := callHRESULTWith(framePool, 7, uintptr(unsafe.Pointer(&frame))); err != nil {
			return nil, capture.Failure("capture_frame_failed", "WGC failed while retrieving a frame", err)
		}
		if frame != nil {
			return frame, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, capture.Failure("capture_timeout", "timed out waiting for a WGC frame", ctx.Err())
			}
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func readFrame(frame, device, context3D unsafe.Pointer, pixelFormat uint32, desc outputDesc1) (image.Image, int, int, error) {
	var surface unsafe.Pointer
	if err := callHRESULTWith(frame, 6, uintptr(unsafe.Pointer(&surface))); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "failed to access the WGC frame surface", err)
	}
	defer release(surface)

	var access unsafe.Pointer
	if err := queryInterface(surface, iidDXGIInterface, &access); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "frame surface does not expose IDirect3DDxgiInterfaceAccess", err)
	}
	defer release(access)

	var sourceTexture unsafe.Pointer
	if err := callHRESULTWith(
		access,
		3,
		uintptr(unsafe.Pointer(iidTexture2D)),
		uintptr(unsafe.Pointer(&sourceTexture)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "failed to obtain the D3D11 frame texture", err)
	}
	defer release(sourceTexture)

	var sourceDesc texture2DDesc
	syscall.SyscallN(
		comMethod(sourceTexture, 10),
		uintptr(sourceTexture),
		uintptr(unsafe.Pointer(&sourceDesc)),
	)
	if sourceDesc.Width == 0 || sourceDesc.Height == 0 {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "captured D3D11 texture has invalid dimensions", nil)
	}
	if sourceDesc.Format != pixelFormat {
		return nil, 0, 0, capture.Failure(
			"capture_format_mismatch",
			fmt.Sprintf("captured D3D11 texture format is %d, expected %d", sourceDesc.Format, pixelFormat),
			nil,
		)
	}

	stagingDesc := texture2DDesc{
		Width:          sourceDesc.Width,
		Height:         sourceDesc.Height,
		MipLevels:      1,
		ArraySize:      1,
		Format:         sourceDesc.Format,
		SampleDesc:     sampleDesc{Count: 1},
		Usage:          d3d11UsageStaging,
		CPUAccessFlags: d3d11CPUAccessRead,
	}
	var staging unsafe.Pointer
	if err := callHRESULTWith(
		device,
		5,
		uintptr(unsafe.Pointer(&stagingDesc)),
		0,
		uintptr(unsafe.Pointer(&staging)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to create the D3D11 staging texture", err)
	}
	defer release(staging)

	syscall.SyscallN(comMethod(context3D, 47), uintptr(context3D), uintptr(staging), uintptr(sourceTexture))
	var mapped mappedSubresource
	if err := callHRESULTWith(
		context3D,
		14,
		uintptr(staging),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&mapped)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to map the D3D11 staging texture", err)
	}
	defer syscall.SyscallN(comMethod(context3D, 15), uintptr(context3D), uintptr(staging), 0)
	if mapped.Data == nil || mapped.RowPitch == 0 {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "mapped D3D11 texture returned an empty buffer", nil)
	}

	bufferSize := uint64(mapped.RowPitch) * uint64(sourceDesc.Height)
	if bufferSize > uint64(math.MaxInt) {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "mapped D3D11 texture is too large", nil)
	}
	raw := make([]byte, int(bufferSize))
	copy(raw, unsafe.Slice((*byte)(mapped.Data), int(bufferSize)))
	width := int(sourceDesc.Width)
	height := int(sourceDesc.Height)

	switch pixelFormat {
	case dxgiFormatB8G8R8A8UNorm:
		converted, convertErr := pixels.BGRA8ToNRGBA(raw, width, height, int(mapped.RowPitch))
		if convertErr != nil {
			return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to convert the SDR frame", convertErr)
		}
		return converted, width, height, nil
	case dxgiFormatR16G16B16A16Float:
		converted, convertErr := pixels.RGBA16FToToneMappedNRGBA(raw, width, height, int(mapped.RowPitch), float64(desc.MaxLuminance))
		if convertErr != nil {
			return nil, 0, 0, capture.Failure("capture_tone_map_failed", "failed to tone-map the HDR frame", convertErr)
		}
		return converted, width, height, nil
	default:
		return nil, 0, 0, capture.Failure("unsupported_capture_format", fmt.Sprintf("unsupported capture format: %d", pixelFormat), nil)
	}
}

func monitorFromDesc(desc outputDesc1) capture.Monitor {
	width := int(desc.DesktopCoordinates.Right - desc.DesktopCoordinates.Left)
	height := int(desc.DesktopCoordinates.Bottom - desc.DesktopCoordinates.Top)
	return capture.Monitor{
		DeviceName:       windows.UTF16ToString(desc.DeviceName[:]),
		Width:            width,
		Height:           height,
		HDR:              desc.ColorSpace == dxgiColorSpaceRGBFullG2084NoneP2020,
		ColorSpace:       colorSpaceName(desc.ColorSpace),
		MaxLuminanceNits: float64(desc.MaxLuminance),
	}
}

func colorSpaceName(value int32) string {
	switch value {
	case dxgiColorSpaceRGBFullG22NoneP709:
		return "RGB_FULL_G22_NONE_P709"
	case dxgiColorSpaceRGBFullG10NoneP709:
		return "RGB_FULL_G10_NONE_P709"
	case dxgiColorSpaceRGBFullG2084NoneP2020:
		return "RGB_FULL_G2084_NONE_P2020"
	default:
		return fmt.Sprintf("DXGI_COLOR_SPACE_%d", value)
	}
}

func finitePositiveAbove80(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 80
}

func queryInterface(object unsafe.Pointer, iid *ole.GUID, result *unsafe.Pointer) error {
	if object == nil {
		return errors.New("QueryInterface object is nil")
	}
	return callHRESULTWith(
		object,
		0,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(result)),
	)
}

func closeAndRelease(object unsafe.Pointer) {
	if object == nil {
		return
	}
	var closable unsafe.Pointer
	if queryInterface(object, iidClosable, &closable) == nil {
		_ = callHRESULT(closable, 6)
		release(closable)
	}
	release(object)
}

func release(object unsafe.Pointer) {
	if object == nil {
		return
	}
	syscall.SyscallN(comMethod(object, 2), uintptr(object))
}

func callHRESULT(object unsafe.Pointer, index int) error {
	return callHRESULTWith(object, index)
}

func callHRESULTWith(object unsafe.Pointer, index int, args ...uintptr) error {
	if object == nil {
		return errors.New("COM object is nil")
	}
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(object))
	callArgs = append(callArgs, args...)
	hr, _, _ := syscall.SyscallN(comMethod(object, index), callArgs...)
	return checkHRESULT(hr, fmt.Sprintf("COM method %d", index))
}

func comMethod(object unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(object)
	return (*[128]uintptr)(vtable)[index]
}

func checkHRESULT(value uintptr, operation string) error {
	if int32(value) < 0 {
		return fmt.Errorf("%s: HRESULT %#08x", operation, uint32(value))
	}
	return nil
}

func factoryPointer(factory *ole.IInspectable) unsafe.Pointer {
	return unsafe.Pointer(factory)
}
