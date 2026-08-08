//go:build windows && amd64

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"golang.org/x/sys/windows"
)

func configureRuntimeDiagnostics(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("runtime stderr log path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create runtime log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open runtime stderr log: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(file.Fd())); err != nil {
		file.Close()
		return nil, fmt.Errorf("redirect Windows stderr handle: %w", err)
	}
	os.Stderr = file
	debug.SetTraceback("all")
	if _, err := fmt.Fprintf(
		file,
		"\n=== windows-capture-agent runtime diagnostics started at %s pid=%d ===\n",
		time.Now().UTC().Format(time.RFC3339Nano),
		os.Getpid(),
	); err != nil {
		file.Close()
		return nil, fmt.Errorf("write runtime diagnostics marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, fmt.Errorf("sync runtime diagnostics marker: %w", err)
	}
	return file, nil
}
