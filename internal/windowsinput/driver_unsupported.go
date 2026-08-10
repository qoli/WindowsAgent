//go:build !windows

package windowsinput

import (
	"context"
	"errors"
)

type WindowsDriver struct{}

func (WindowsDriver) Press(context.Context, PressRequest) (Evidence, error) {
	return Evidence{}, errors.New("Windows input injection is supported only on Windows")
}

func (WindowsDriver) KeyDown(context.Context, KeyRequest) (Evidence, error) {
	return Evidence{}, errors.New("Windows input injection is supported only on Windows")
}

func (WindowsDriver) KeyUp(context.Context, KeyRequest) (Evidence, error) {
	return Evidence{}, errors.New("Windows input injection is supported only on Windows")
}

func (WindowsDriver) ClickReference(context.Context, PointerClickRequest) (PointerEvidence, error) {
	return PointerEvidence{}, errors.New("Windows pointer injection is supported only on Windows")
}
