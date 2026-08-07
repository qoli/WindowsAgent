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
