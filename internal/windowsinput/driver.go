// Package windowsinput defines game-neutral Windows input injection.
package windowsinput

import (
	"context"
	"fmt"
	"time"
)

const BackendSendInputScanCode = "sendinput-scancode"
const BackendSendInputPointer = "sendinput-pointer"

type PressRequest struct {
	Key  string
	Hold time.Duration
}

type KeyRequest struct {
	Key string
}

type Evidence struct {
	Backend  string
	Key      string
	ScanCode uint16
	Extended bool
	HoldMS   int64
}

// ReleaseError reports that a key-down was sent but the compensating key-up
// failed. Callers must treat this as a safety failure even when cancellation
// triggered the release attempt.
type ReleaseError struct {
	Cause error
}

func (e *ReleaseError) Error() string {
	if e == nil || e.Cause == nil {
		return "release input key"
	}
	return fmt.Sprintf("release input key: %v", e.Cause)
}

func (e *ReleaseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type PointerClickRequest struct {
	ReferenceX int
	ReferenceY int
	Hold       time.Duration
}

type CurrentPointerClickRequest struct {
	Hold time.Duration
}

type PointerEvidence struct {
	Backend        string
	ReferenceX     int
	ReferenceY     int
	ScreenX        int
	ScreenY        int
	ScreenWidth    int
	ScreenHeight   int
	ViewportX      int
	ViewportY      int
	ViewportWidth  int
	ViewportHeight int
	HoldMS         int64
}

type Driver interface {
	Press(context.Context, PressRequest) (Evidence, error)
	KeyDown(context.Context, KeyRequest) (Evidence, error)
	KeyUp(context.Context, KeyRequest) (Evidence, error)
}
