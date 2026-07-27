//go:build windows && amd64

package scriptrunner

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

type windowsNativeBackend struct{}

type windowsNativeDLL struct {
	library *windows.LazyDLL
	handle  windows.Handle
	closed  bool
}

type windowsNativeProcedure struct {
	procedure *windows.LazyProc
}

func newNativeBackend() nativeBackend {
	return windowsNativeBackend{}
}

func (windowsNativeBackend) load(path string) (nativeDLL, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("native library artifact is not a regular file")
	}
	library := windows.NewLazyDLL(path)
	if err := library.Load(); err != nil {
		return nil, fmt.Errorf("LoadLibraryW: %w", err)
	}
	return &windowsNativeDLL{library: library, handle: windows.Handle(library.Handle())}, nil
}

func (l *windowsNativeDLL) bind(name string) (nativeProcedure, error) {
	if l.closed {
		return nil, errors.New("native library is closed")
	}
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return nil, errors.New("native export name is invalid")
	}
	procedure := l.library.NewProc(name)
	if err := procedure.Find(); err != nil {
		return nil, fmt.Errorf("GetProcAddress %q: %w", name, err)
	}
	return &windowsNativeProcedure{procedure: procedure}, nil
}

func (l *windowsNativeDLL) close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	return windows.FreeLibrary(l.handle)
}

func (p *windowsNativeProcedure) call(frame nativeCallFrame) (uintptr, error) {
	result, _, _ := p.procedure.Call(frame.arguments...)
	return result, nil
}
