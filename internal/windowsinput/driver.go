// Package windowsinput defines game-neutral Windows input injection.
package windowsinput

import (
	"context"
	"time"
)

const BackendSendInputScanCode = "sendinput-scancode"

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

type Driver interface {
	Press(context.Context, PressRequest) (Evidence, error)
	KeyDown(context.Context, KeyRequest) (Evidence, error)
	KeyUp(context.Context, KeyRequest) (Evidence, error)
}
