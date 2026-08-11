//go:build windows && amd64

package mfvideo

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/whiteboxsolutions/go-ole"
	"golang.org/x/sys/windows"
)

const (
	mfVersion                   = 0x00020070
	mfStartupFull               = 0
	mfVideoInterlaceProgressive = 2
	hundredNanosecondsPerSecond = 10_000_000
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	mfMediaTypeVideo     = guid{0x73646976, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfVideoFormatH264    = guid{0x34363248, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfVideoFormatRGB32   = guid{0x00000016, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}}
	mfMTMajorType        = guid{0x48eba18e, 0xf8c9, 0x4687, [8]byte{0xbf, 0x11, 0x0a, 0x74, 0xc9, 0xf9, 0x6a, 0x8f}}
	mfMTSubtype          = guid{0xf7e34c9a, 0x42e8, 0x4714, [8]byte{0xb7, 0x4b, 0xcb, 0x29, 0xd7, 0x2c, 0x35, 0xe5}}
	mfMTAvgBitrate       = guid{0x20332624, 0xfb0d, 0x4d9e, [8]byte{0xbd, 0x0d, 0xcb, 0xf6, 0x78, 0x6c, 0x10, 0x2e}}
	mfMTFrameRate        = guid{0xc459a2e8, 0x3d2c, 0x4e44, [8]byte{0xb1, 0x32, 0xfe, 0xe5, 0x15, 0x6c, 0x7b, 0xb0}}
	mfMTFrameSize        = guid{0x1652c33d, 0xd6b2, 0x4012, [8]byte{0xb8, 0x34, 0x72, 0x03, 0x08, 0x49, 0xa3, 0x7d}}
	mfMTInterlaceMode    = guid{0xe2724bb8, 0xe676, 0x4806, [8]byte{0xb4, 0xb2, 0xa8, 0xd6, 0xef, 0xb4, 0x4c, 0xcd}}
	mfMTPixelAspectRatio = guid{0xc6376a1e, 0x8d0a, 0x4027, [8]byte{0xbe, 0x45, 0x6d, 0x9a, 0x0a, 0xd3, 0x9b, 0xb6}}

	modMFPlat                     = windows.NewLazySystemDLL("mfplat.dll")
	modMFReadWrite                = windows.NewLazySystemDLL("mfreadwrite.dll")
	procMFStartup                 = modMFPlat.NewProc("MFStartup")
	procMFShutdown                = modMFPlat.NewProc("MFShutdown")
	procMFCreateMediaType         = modMFPlat.NewProc("MFCreateMediaType")
	procMFCreateMemoryBuffer      = modMFPlat.NewProc("MFCreateMemoryBuffer")
	procMFCreateSample            = modMFPlat.NewProc("MFCreateSample")
	procMFCreateSinkWriterFromURL = modMFReadWrite.NewProc("MFCreateSinkWriterFromURL")
)

type command struct {
	ctx    context.Context
	pixels []byte
	index  uint64
	finish bool
	result chan error
}

// Encoder owns one Media Foundation sink writer on one locked OS thread.
type Encoder struct {
	format   Format
	commands chan command
	done     chan struct{}
}

func NewEncoder(path string, format Format) (*Encoder, error) {
	if path == "" {
		return nil, errors.New("Media Foundation output path is required")
	}
	if err := format.Validate(); err != nil {
		return nil, err
	}
	encoder := &Encoder{format: format, commands: make(chan command), done: make(chan struct{})}
	ready := make(chan error, 1)
	go encoder.worker(path, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return encoder, nil
}

func (e *Encoder) WriteFrame(ctx context.Context, index uint64, pixels []byte) error {
	if e == nil || index == 0 {
		return errors.New("Media Foundation encoder and one-based frame index are required")
	}
	if len(pixels) != e.format.FrameBytes() {
		return fmt.Errorf("Media Foundation frame has %d bytes, expected %d", len(pixels), e.format.FrameBytes())
	}
	owned := append([]byte(nil), pixels...)
	result := make(chan error, 1)
	select {
	case e.commands <- command{ctx: ctx, pixels: owned, index: index, result: result}:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return errors.New("Media Foundation encoder is closed")
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Encoder) Finalize(ctx context.Context) error {
	if e == nil {
		return errors.New("Media Foundation encoder is required")
	}
	result := make(chan error, 1)
	select {
	case e.commands <- command{ctx: ctx, finish: true, result: result}:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return errors.New("Media Foundation encoder is already closed")
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Encoder) worker(path string, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(e.done)
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		ready <- fmt.Errorf("initialize COM for Media Foundation: %w", err)
		return
	}
	defer ole.CoUninitialize()
	if err := callProc("MFStartup", procMFStartup, mfVersion, mfStartupFull); err != nil {
		ready <- err
		return
	}
	defer func() { _, _, _ = procMFShutdown.Call() }()
	writer, stream, err := createSinkWriter(path, e.format)
	if err != nil {
		ready <- err
		return
	}
	defer release(writer)
	ready <- nil
	for cmd := range e.commands {
		if cmd.finish {
			cmd.result <- callMethod("IMFSinkWriter.Finalize", writer, 11)
			return
		}
		if err := cmd.ctx.Err(); err != nil {
			cmd.result <- err
			continue
		}
		cmd.result <- writeFrame(writer, stream, e.format, cmd.index, cmd.pixels)
	}
}

func createSinkWriter(path string, format Format) (unsafe.Pointer, uint32, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, 0, err
	}
	var writer unsafe.Pointer
	if err = callProc("MFCreateSinkWriterFromURL", procMFCreateSinkWriterFromURL, uintptr(unsafe.Pointer(wide)), 0, 0, uintptr(unsafe.Pointer(&writer))); err != nil {
		return nil, 0, err
	}
	ok := false
	defer func() {
		if !ok {
			release(writer)
		}
	}()
	output, err := createMediaType()
	if err != nil {
		return nil, 0, err
	}
	defer release(output)
	for _, attribute := range []struct {
		name  string
		key   *guid
		value *guid
	}{
		{"output major type", &mfMTMajorType, &mfMediaTypeVideo},
		{"output subtype", &mfMTSubtype, &mfVideoFormatH264},
	} {
		if err = setGUID(output, attribute.name, attribute.key, attribute.value); err != nil {
			return nil, 0, err
		}
	}
	for _, attribute := range []struct {
		name  string
		key   *guid
		value uint32
	}{
		{"output bitrate", &mfMTAvgBitrate, format.Bitrate},
		{"output interlace", &mfMTInterlaceMode, mfVideoInterlaceProgressive},
	} {
		if err = setUINT32(output, attribute.name, attribute.key, attribute.value); err != nil {
			return nil, 0, err
		}
	}
	if err = setUINT64(output, "output frame size", &mfMTFrameSize, packPair(uint32(format.Width), uint32(format.Height))); err != nil {
		return nil, 0, err
	}
	if err = setUINT64(output, "output frame rate", &mfMTFrameRate, packPair(uint32(format.FramesPerSecond), 1)); err != nil {
		return nil, 0, err
	}
	if err = setUINT64(output, "output pixel aspect", &mfMTPixelAspectRatio, packPair(1, 1)); err != nil {
		return nil, 0, err
	}
	var stream uint32
	if err = callMethod("IMFSinkWriter.AddStream", writer, 3, uintptr(output), uintptr(unsafe.Pointer(&stream))); err != nil {
		return nil, 0, err
	}
	input, err := createMediaType()
	if err != nil {
		return nil, 0, err
	}
	defer release(input)
	for _, attribute := range []struct {
		name  string
		key   *guid
		value *guid
	}{
		{"input major type", &mfMTMajorType, &mfMediaTypeVideo},
		{"input subtype", &mfMTSubtype, &mfVideoFormatRGB32},
	} {
		if err = setGUID(input, attribute.name, attribute.key, attribute.value); err != nil {
			return nil, 0, err
		}
	}
	if err = setUINT32(input, "input interlace", &mfMTInterlaceMode, mfVideoInterlaceProgressive); err != nil {
		return nil, 0, err
	}
	if err = setUINT64(input, "input frame size", &mfMTFrameSize, packPair(uint32(format.Width), uint32(format.Height))); err != nil {
		return nil, 0, err
	}
	if err = setUINT64(input, "input frame rate", &mfMTFrameRate, packPair(uint32(format.FramesPerSecond), 1)); err != nil {
		return nil, 0, err
	}
	if err = setUINT64(input, "input pixel aspect", &mfMTPixelAspectRatio, packPair(1, 1)); err != nil {
		return nil, 0, err
	}
	if err = callMethod("IMFSinkWriter.SetInputMediaType", writer, 4, uintptr(stream), uintptr(input), 0); err != nil {
		return nil, 0, err
	}
	if err = callMethod("IMFSinkWriter.BeginWriting", writer, 5); err != nil {
		return nil, 0, err
	}
	ok = true
	return writer, stream, nil
}

func writeFrame(writer unsafe.Pointer, stream uint32, format Format, index uint64, pixels []byte) error {
	var buffer unsafe.Pointer
	if err := callProc("MFCreateMemoryBuffer", procMFCreateMemoryBuffer, uintptr(len(pixels)), uintptr(unsafe.Pointer(&buffer))); err != nil {
		return err
	}
	defer release(buffer)
	var data *byte
	var maximum, current uint32
	if err := callMethod("IMFMediaBuffer.Lock", buffer, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&maximum)), uintptr(unsafe.Pointer(&current))); err != nil {
		return err
	}
	if maximum < uint32(len(pixels)) {
		_ = callMethod("IMFMediaBuffer.Unlock", buffer, 4)
		return errors.New("Media Foundation buffer is smaller than the video frame")
	}
	copy(unsafe.Slice(data, len(pixels)), pixels)
	if err := callMethod("IMFMediaBuffer.Unlock", buffer, 4); err != nil {
		return err
	}
	if err := callMethod("IMFMediaBuffer.SetCurrentLength", buffer, 6, uintptr(len(pixels))); err != nil {
		return err
	}
	var sample unsafe.Pointer
	if err := callProc("MFCreateSample", procMFCreateSample, uintptr(unsafe.Pointer(&sample))); err != nil {
		return err
	}
	defer release(sample)
	if err := callMethod("IMFSample.AddBuffer", sample, 42, uintptr(buffer)); err != nil {
		return err
	}
	start := int64(index-1) * hundredNanosecondsPerSecond / int64(format.FramesPerSecond)
	duration := int64(hundredNanosecondsPerSecond / format.FramesPerSecond)
	if err := callMethod("IMFSample.SetSampleTime", sample, 36, uintptr(start)); err != nil {
		return err
	}
	if err := callMethod("IMFSample.SetSampleDuration", sample, 38, uintptr(duration)); err != nil {
		return err
	}
	return callMethod("IMFSinkWriter.WriteSample", writer, 6, uintptr(stream), uintptr(sample))
}

func createMediaType() (unsafe.Pointer, error) {
	var mediaType unsafe.Pointer
	if err := callProc("MFCreateMediaType", procMFCreateMediaType, uintptr(unsafe.Pointer(&mediaType))); err != nil {
		return nil, err
	}
	return mediaType, nil
}

func setGUID(object unsafe.Pointer, name string, key, value *guid) error {
	return callMethod("IMFAttributes.SetGUID "+name, object, 24, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(value)))
}

func setUINT32(object unsafe.Pointer, name string, key *guid, value uint32) error {
	return callMethod("IMFAttributes.SetUINT32 "+name, object, 21, uintptr(unsafe.Pointer(key)), uintptr(value))
}

func setUINT64(object unsafe.Pointer, name string, key *guid, value uint64) error {
	return callMethod("IMFAttributes.SetUINT64 "+name, object, 22, uintptr(unsafe.Pointer(key)), uintptr(value))
}

func packPair(first, second uint32) uint64 { return uint64(first)<<32 | uint64(second) }

func callProc(name string, proc *windows.LazyProc, args ...uintptr) error {
	result, _, _ := proc.Call(args...)
	return hresult(name, result)
}

func callMethod(name string, object unsafe.Pointer, index uintptr, args ...uintptr) error {
	if object == nil {
		return fmt.Errorf("%s: COM object is nil", name)
	}
	arguments := append([]uintptr{uintptr(object)}, args...)
	result, _, _ := syscall.SyscallN(comMethod(object, index), arguments...)
	return hresult(name, result)
}

func hresult(name string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("%s failed with HRESULT 0x%08x", name, uint32(result))
	}
	return nil
}

func comMethod(object unsafe.Pointer, index uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(object)
	return *(*uintptr)(unsafe.Add(vtable, index*unsafe.Sizeof(uintptr(0))))
}

func release(object unsafe.Pointer) {
	if object != nil {
		_, _, _ = syscall.SyscallN(comMethod(object, 2), uintptr(object))
	}
}
