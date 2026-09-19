package assisttailscale

import (
	"errors"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/assistgui"
)

func TestReconcileStatusRejectsStaleOnlineProcessIdentity(t *testing.T) {
	status := assistgui.TailscaleSnapshot{
		SchemaVersion: 1,
		Enabled:       true,
		State:         assistgui.TailscaleOnline,
		IPv4:          "100.64.0.1",
		ProcessID:     42,
		Generation:    "0123456789abcdef0123456789abcdef",
	}
	got := reconcileStatus(status, nil, nil)
	if got.State != assistgui.TailscaleFailed || got.IPv4 != "" || !strings.Contains(got.Error, "0 installed processes") {
		t.Fatalf("reconcileStatus(stale online) = %#v", got)
	}
}

func TestReconcileStatusRequiresExactInteractiveProcess(t *testing.T) {
	status := assistgui.TailscaleSnapshot{
		SchemaVersion: 1,
		Enabled:       true,
		State:         assistgui.TailscaleOnline,
		ProcessID:     42,
		Generation:    "0123456789abcdef0123456789abcdef",
	}
	for _, processes := range [][]adapterProcess{
		{{PID: 41, SessionID: 1}},
		{{PID: 42, SessionID: 0}},
	} {
		got := reconcileStatus(status, processes, nil)
		if got.State != assistgui.TailscaleFailed || !strings.Contains(got.Error, "interactive-session process") {
			t.Fatalf("reconcileStatus(%v) = %#v", processes, got)
		}
	}
	got := reconcileStatus(status, []adapterProcess{{PID: 42, SessionID: 1}}, nil)
	if got.State != assistgui.TailscaleOnline || got.ProcessID != 42 {
		t.Fatalf("reconcileStatus(exact process) = %#v", got)
	}
}

func TestReconcileStatusFailsProcessInspectionExplicitly(t *testing.T) {
	got := reconcileStatus(
		assistgui.TailscaleSnapshot{SchemaVersion: 1, Enabled: false, State: assistgui.TailscaleDisabled},
		nil,
		errors.New("snapshot unavailable"),
	)
	if got.State != assistgui.TailscaleFailed || !strings.Contains(got.Error, "snapshot unavailable") {
		t.Fatalf("reconcileStatus(inspect error) = %#v", got)
	}
}
