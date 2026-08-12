package wgcworker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

func TestDecodeStrictRejectsUnknownAndTrailingFields(t *testing.T) {
	var params initializeParams
	if err := decodeStrict(json.RawMessage(`{"protocolVersion":"x","trace":false,"deadline":"2026-08-12T00:00:00Z","extra":true}`), &params); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error=%v", err)
	}
	if err := decodeStrict(json.RawMessage(`{"protocolVersion":"x","trace":false,"deadline":"2026-08-12T00:00:00Z"} {}`), &params); err == nil || !strings.Contains(err.Error(), "multiple JSON") {
		t.Fatalf("trailing-value error=%v", err)
	}
}

func TestValidateInitializeResultRequiresVerifiedBorderlessSession(t *testing.T) {
	valid := initializeResult{
		ProtocolVersion:  ProtocolVersion,
		ProcessID:        41,
		Backend:          "windows-graphics-capture",
		Persistent:       true,
		BorderlessAccess: "allowed",
		Status:           capture.Status{Supported: true},
	}
	if err := validateInitializeResult(valid, 41); err != nil {
		t.Fatal(err)
	}
	bordered := valid
	bordered.BorderRequired = true
	if err := validateInitializeResult(bordered, 41); err == nil || !strings.Contains(err.Error(), "borderRequired") {
		t.Fatalf("bordered initialize result error=%v", err)
	}
	denied := valid
	denied.BorderlessAccess = "denied"
	if err := validateInitializeResult(denied, 41); err == nil || !strings.Contains(err.Error(), "borderlessAccess") {
		t.Fatalf("denied initialize result error=%v", err)
	}
}

func TestEffectiveInitializationDeadlineIsBounded(t *testing.T) {
	started := time.Now()
	deadline, err := effectiveInitializationDeadline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deadline.Before(started.Add(MaxInitializationDuration-time.Second)) || deadline.After(started.Add(MaxInitializationDuration+time.Second)) {
		t.Fatalf("initialization deadline=%s", deadline)
	}
}

func TestEffectiveDeadlineUsesEarlierContextDeadline(t *testing.T) {
	wanted := time.Now().Add(100 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), wanted)
	defer cancel()
	deadline, err := effectiveDeadline(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if deadline.After(wanted) {
		t.Fatalf("deadline=%s wanted no later than %s", deadline, wanted)
	}
}
