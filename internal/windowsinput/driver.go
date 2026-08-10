// Package windowsinput defines game-neutral Windows input injection.
package windowsinput

import (
	"context"
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

type PointerClickRequest struct {
	ReferenceX int
	ReferenceY int
	Hold       time.Duration
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
