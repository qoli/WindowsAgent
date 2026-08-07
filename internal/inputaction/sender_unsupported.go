//go:build !windows

package inputaction

import (
	"context"
	"errors"
)

type WindowsSender struct{}

func (WindowsSender) Press(context.Context, uint16) error {
	return errors.New("keyboard input injection is supported only on Windows")
}
