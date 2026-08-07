//go:build windows

package ocrworker

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConfigureWorkerCommandSuppressesConsoleWindow(t *testing.T) {
	command := exec.Command("PpOcr.DirectML.exe", "--worker")
	configureWorkerCommand(command)

	if command.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow is false")
	}
	if command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", command.SysProcAttr.CreationFlags)
	}
}
