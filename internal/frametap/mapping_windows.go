//go:build windows && amd64

package frametap

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qoli/WindowsAgent/internal/videocapture"
	"golang.org/x/sys/windows"
)

const (
	fileMapRead   = 0x0004
	fileMapWrite  = 0x0002
	pageReadWrite = 0x04
	foregroundMax = 512
)

var mappingMagic = [8]byte{'W', 'A', 'F', 'R', 'M', 'T', '0', '1'}

var procOpenFileMappingW = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenFileMappingW")

type mapping struct {
	handle windows.Handle
	view   uintptr
	bytes  []byte
	write  bool
}

func CreatePublisher(name string) (Publisher, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	wide, _ := windows.UTF16PtrFromString(name)
	handle, err := windows.CreateFileMapping(windows.InvalidHandle, nil, pageReadWrite, 0, MappingBytes, wide)
	if err != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return nil, fmt.Errorf("create frame tap mapping: %w", err)
	}
	view, err := windows.MapViewOfFile(handle, fileMapRead|fileMapWrite, 0, 0, MappingBytes)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("map frame tap publisher: %w", err)
	}
	return &mapping{handle: handle, view: view, bytes: mappedBytes(view), write: true}, nil
}

func OpenReader(name string) (Reader, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	wide, _ := windows.UTF16PtrFromString(name)
	raw, _, callErr := procOpenFileMappingW.Call(fileMapRead, 0, uintptr(unsafe.Pointer(wide)))
	if raw == 0 {
		return nil, fmt.Errorf("open frame tap mapping: %w", callErr)
	}
	handle := windows.Handle(raw)
	view, err := windows.MapViewOfFile(handle, fileMapRead, 0, 0, MappingBytes)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("map frame tap reader: %w", err)
	}
	return &mapping{handle: handle, view: view, bytes: mappedBytes(view)}, nil
}

func mappedBytes(view uintptr) []byte {
	var data []byte
	header := (*reflect.SliceHeader)(unsafe.Pointer(&data))
	header.Data = view
	header.Len = MappingBytes
	header.Cap = MappingBytes
	return data
}

func (m *mapping) Publish(ctx context.Context, frame videocapture.Frame) error {
	if m == nil || !m.write || m.view == 0 {
		return errors.New("frame tap publisher is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := frame.Validate(); err != nil {
		return err
	}
	foreground := []byte(frame.ForegroundExecutable)
	if len(foreground) == 0 || len(foreground) > foregroundMax {
		return errors.New("frame tap foreground executable is invalid")
	}
	generation := (*uint64)(unsafe.Pointer(&m.bytes[16]))
	start := atomic.AddUint64(generation, 1)
	if start%2 == 0 {
		start = atomic.AddUint64(generation, 1)
	}
	copy(m.bytes[0:8], mappingMagic[:])
	binary.LittleEndian.PutUint32(m.bytes[8:12], 1)
	binary.LittleEndian.PutUint32(m.bytes[12:16], HeaderBytes)
	binary.LittleEndian.PutUint64(m.bytes[24:32], uint64(frame.ScheduledAt.UnixNano()))
	binary.LittleEndian.PutUint64(m.bytes[32:40], uint64(frame.ObservedAt.UnixNano()))
	binary.LittleEndian.PutUint64(m.bytes[40:48], frame.Sequence)
	binary.LittleEndian.PutUint32(m.bytes[48:52], Width)
	binary.LittleEndian.PutUint32(m.bytes[52:56], Height)
	binary.LittleEndian.PutUint32(m.bytes[56:60], 1)
	binary.LittleEndian.PutUint32(m.bytes[60:64], uint32(len(foreground)))
	binary.LittleEndian.PutUint32(m.bytes[64:68], PixelBytes)
	clear(m.bytes[128 : 128+foregroundMax])
	copy(m.bytes[128:128+foregroundMax], foreground)
	copy(m.bytes[HeaderBytes:], frame.Pixels)
	atomic.StoreUint64(generation, start+1)
	return nil
}

func (m *mapping) Latest(ctx context.Context, after time.Time) (videocapture.Frame, error) {
	if m == nil || m.write || m.view == 0 {
		return videocapture.Frame{}, errors.New("frame tap reader is closed")
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err := ctx.Err(); err != nil {
			return videocapture.Frame{}, err
		}
		generation := atomic.LoadUint64((*uint64)(unsafe.Pointer(&m.bytes[16])))
		if generation == 0 {
			return videocapture.Frame{}, ErrNoFrame
		}
		if generation%2 != 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		header := append([]byte(nil), m.bytes[:HeaderBytes]...)
		pixels := append([]byte(nil), m.bytes[HeaderBytes:]...)
		if generation != atomic.LoadUint64((*uint64)(unsafe.Pointer(&m.bytes[16]))) {
			continue
		}
		frame, err := decodeFrame(header, pixels)
		if err != nil {
			return videocapture.Frame{}, err
		}
		if !frame.ScheduledAt.After(after) {
			return videocapture.Frame{}, ErrNoNewFrame
		}
		return frame, nil
	}
	return videocapture.Frame{}, errors.New("frame tap changed during every read attempt")
}

func decodeFrame(header, pixels []byte) (videocapture.Frame, error) {
	if len(header) != HeaderBytes || len(pixels) != PixelBytes || string(header[:8]) != string(mappingMagic[:]) || binary.LittleEndian.Uint32(header[8:12]) != 1 || binary.LittleEndian.Uint32(header[12:16]) != HeaderBytes || binary.LittleEndian.Uint32(header[48:52]) != Width || binary.LittleEndian.Uint32(header[52:56]) != Height || binary.LittleEndian.Uint32(header[56:60]) != 1 || binary.LittleEndian.Uint32(header[64:68]) != PixelBytes {
		return videocapture.Frame{}, errors.New("frame tap header identity is invalid")
	}
	length := int(binary.LittleEndian.Uint32(header[60:64]))
	if length < 1 || length > foregroundMax {
		return videocapture.Frame{}, errors.New("frame tap foreground length is invalid")
	}
	frame := videocapture.Frame{Sequence: binary.LittleEndian.Uint64(header[40:48]), ScheduledAt: time.Unix(0, int64(binary.LittleEndian.Uint64(header[24:32]))).UTC(), ObservedAt: time.Unix(0, int64(binary.LittleEndian.Uint64(header[32:40]))).UTC(), ForegroundExecutable: string(header[128 : 128+length]), Width: Width, Height: Height, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: pixels}
	if err := frame.Validate(); err != nil {
		return videocapture.Frame{}, err
	}
	return frame, nil
}

func (m *mapping) Close() error {
	if m == nil || m.view == 0 {
		return nil
	}
	view, handle := m.view, m.handle
	m.view, m.handle, m.bytes = 0, 0, nil
	unmapErr := windows.UnmapViewOfFile(view)
	closeErr := windows.CloseHandle(handle)
	return errors.Join(unmapErr, closeErr)
}
