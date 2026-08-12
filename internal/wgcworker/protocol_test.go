package wgcworker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeStrictRejectsUnknownAndTrailingFields(t *testing.T) {
	var params initializeParams
	if err := decodeStrict(json.RawMessage(`{"protocolVersion":"x","trace":false,"extra":true}`), &params); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error=%v", err)
	}
	if err := decodeStrict(json.RawMessage(`{"protocolVersion":"x","trace":false} {}`), &params); err == nil || !strings.Contains(err.Error(), "multiple JSON") {
		t.Fatalf("trailing-value error=%v", err)
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
