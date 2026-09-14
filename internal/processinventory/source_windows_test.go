//go:build windows

package processinventory

import (
	"context"
	"os"
	"testing"
)

func TestServiceTypeMatchesOsqueryNames(t *testing.T) {
	tests := map[uint32]string{
		0x00000001: "KERNEL_DRIVER",
		0x00000002: "FILE_SYSTEM_DRIVER",
		0x00000010: "OWN_PROCESS",
		0x00000020: "SHARE_PROCESS",
		0x00000050: "USER_OWN_PROCESS",
		0x00000060: "USER_SHARE_PROCESS",
		0x000000d0: "USER_OWN_PROCESS(Instance)",
		0x000000e0: "USER_SHARE_PROCESS(Instance)",
		0x00000100: "INTERACTIVE_PROCESS",
		0x00000110: "OWN_PROCESS(Interactive)",
		0x00000120: "SHARE_PROCESS(Interactive)",
	}
	for input, want := range tests {
		if got := serviceType(input); got != want {
			t.Fatalf("serviceType(0x%08X) = %q, want %q", input, got, want)
		}
	}
	if got := serviceType(0xffffffff); got != "UNKNOWN" {
		t.Fatalf("unknown service type = %q", got)
	}
}

func TestOSCollectorReturnsLiveWindowsTables(t *testing.T) {
	snapshot, err := NewOSCollector().Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Processes) == 0 || len(snapshot.Services) == 0 {
		t.Fatalf("processes = %d, services = %d", len(snapshot.Processes), len(snapshot.Services))
	}
	currentPID := uint32(os.Getpid())
	var current *Process
	for index := range snapshot.Processes {
		if snapshot.Processes[index].PID == currentPID {
			current = &snapshot.Processes[index]
			break
		}
	}
	if current == nil {
		t.Fatalf("current process %d is missing", currentPID)
	}
	if current.Name == "" || current.Path == nil || current.Cmdline == nil || current.State == nil || current.StartTime == nil || current.SessionID == nil {
		t.Fatalf("current process enrichment = %+v", *current)
	}
}
