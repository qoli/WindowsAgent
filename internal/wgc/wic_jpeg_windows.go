//go:build windows && amd64

package wgc

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"

	win32 "github.com/lxn/win"
	"github.com/whiteboxsolutions/go-ole"
	"golang.org/x/sys/windows"
)

const (
	wicBitmapEncoderNoCache = 2
	variantFloat32          = 4
	variantUint8            = 17
	jpegSubsampling444      = 3
)

var (
	modOle32                   = windows.NewLazySystemDLL("ole32.dll")
	modKernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procCreateStreamOnHGlobal  = modOle32.NewProc("CreateStreamOnHGlobal")
	procGetHGlobalFromStream   = modOle32.NewProc("GetHGlobalFromStream")
	procGlobalSize             = modKernel32.NewProc("GlobalSize")
	clsidWICImagingFactory     = ole.NewGUID("{CACAF262-9370-4615-A13B-9F5539DA4C0A}")
	iidWICImagingFactory       = ole.NewGUID("{EC5EC8A9-C395-4314-9C77-54D7A935FF70}")
	guidContainerFormatJPEG    = ole.NewGUID("{19E4A5AA-5662-4FC5-A0C0-1758028E1057}")
	guidWICPixelFormat24bppBGR = ole.NewGUID("{6FDDC324-4E03-4BFE-B185-3D77768DC90C}")
)

type propertyBag2 struct {
	Type       uint32
	VarType    uint16
	ClipFormat uint16
	Hint       uint32
	Padding    uint32
	Name       *uint16
	CLSID      ole.GUID
}

type variant struct {
	Type     uint16
	Reserved [3]uint16
	Data     [2]uint64
}

func encodeWICJPEG(bgr []byte, width, height, quality int) ([]byte, error) {
	if width <= 0 || height <= 0 || len(bgr) != width*height*3 {
		return nil, errors.New("JPEG input dimensions and BGR payload are inconsistent")
	}
	if quality != 90 {
		return nil, fmt.Errorf("unsupported JPEG quality %d", quality)
	}

	factoryUnknown, err := ole.CreateInstance(clsidWICImagingFactory, iidWICImagingFactory)
	if err != nil {
		return nil, fmt.Errorf("create WIC imaging factory: %w", err)
	}
	factory := unsafe.Pointer(factoryUnknown)
	defer release(factory)

	var encoder unsafe.Pointer
	if err := callHRESULTWith(factory, 8,
		uintptr(unsafe.Pointer(guidContainerFormatJPEG)), 0, uintptr(unsafe.Pointer(&encoder))); err != nil {
		return nil, fmt.Errorf("create WIC JPEG encoder: %w", err)
	}
	defer release(encoder)

	var stream unsafe.Pointer
	hr, _, _ := procCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if err := checkHRESULT(hr, "CreateStreamOnHGlobal"); err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, errors.New("CreateStreamOnHGlobal returned a nil stream")
	}
	defer release(stream)

	if err := callHRESULTWith(encoder, 3, uintptr(stream), wicBitmapEncoderNoCache); err != nil {
		return nil, fmt.Errorf("initialize WIC JPEG encoder: %w", err)
	}

	var frame, options unsafe.Pointer
	if err := callHRESULTWith(encoder, 10,
		uintptr(unsafe.Pointer(&frame)), uintptr(unsafe.Pointer(&options))); err != nil {
		return nil, fmt.Errorf("create WIC JPEG frame: %w", err)
	}
	defer release(frame)
	defer release(options)
	if frame == nil || options == nil {
		return nil, errors.New("WIC JPEG encoder did not return a frame and property bag")
	}

	qualityName, _ := windows.UTF16PtrFromString("ImageQuality")
	subsamplingName, _ := windows.UTF16PtrFromString("JpegYCrCbSubsampling")
	properties := [2]propertyBag2{{Name: qualityName}, {Name: subsamplingName}}
	values := [2]variant{
		{Type: variantFloat32, Data: [2]uint64{uint64(math.Float32bits(float32(quality) / 100)), 0}},
		{Type: variantUint8, Data: [2]uint64{jpegSubsampling444, 0}},
	}
	if err := callHRESULTWith(options, 4, 2,
		uintptr(unsafe.Pointer(&properties[0])), uintptr(unsafe.Pointer(&values[0]))); err != nil {
		return nil, fmt.Errorf("set WIC JPEG quality and 4:4:4 subsampling: %w", err)
	}
	if err := callHRESULTWith(frame, 3, uintptr(options)); err != nil {
		return nil, fmt.Errorf("initialize WIC JPEG frame: %w", err)
	}
	if err := callHRESULTWith(frame, 4, uintptr(width), uintptr(height)); err != nil {
		return nil, fmt.Errorf("set WIC JPEG dimensions: %w", err)
	}
	pixelFormat := *guidWICPixelFormat24bppBGR
	if err := callHRESULTWith(frame, 6, uintptr(unsafe.Pointer(&pixelFormat))); err != nil {
		return nil, fmt.Errorf("set WIC JPEG pixel format: %w", err)
	}
	if pixelFormat != *guidWICPixelFormat24bppBGR {
		return nil, errors.New("WIC JPEG encoder rejected 24-bit BGR input")
	}
	stride := width * 3
	if err := callHRESULTWith(frame, 10,
		uintptr(height), uintptr(stride), uintptr(len(bgr)), uintptr(unsafe.Pointer(&bgr[0]))); err != nil {
		return nil, fmt.Errorf("write WIC JPEG pixels: %w", err)
	}
	runtime.KeepAlive(bgr)
	runtime.KeepAlive(properties)
	runtime.KeepAlive(values)

	if err := callHRESULT(frame, 12); err != nil {
		return nil, fmt.Errorf("commit WIC JPEG frame: %w", err)
	}
	if err := callHRESULT(encoder, 11); err != nil {
		return nil, fmt.Errorf("commit WIC JPEG encoder: %w", err)
	}

	var globalMemory uintptr
	hr, _, _ = procGetHGlobalFromStream.Call(uintptr(stream), uintptr(unsafe.Pointer(&globalMemory)))
	if err := checkHRESULT(hr, "GetHGlobalFromStream"); err != nil {
		return nil, err
	}
	size, _, _ := procGlobalSize.Call(globalMemory)
	if size == 0 || size > uintptr(^uint(0)>>1) {
		return nil, errors.New("WIC JPEG output has an invalid size")
	}
	locked := win32.GlobalLock(win32.HGLOBAL(globalMemory))
	if locked == nil {
		return nil, errors.New("GlobalLock(WIC JPEG output) failed")
	}
	content := append([]byte(nil), unsafe.Slice((*byte)(locked), int(size))...)
	win32.GlobalUnlock(win32.HGLOBAL(globalMemory))
	return content, nil
}
