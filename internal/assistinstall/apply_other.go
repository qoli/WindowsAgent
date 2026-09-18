//go:build !windows

package assistinstall

import (
	"context"
	"errors"
)

func runInstaller(context.Context, Request, string) error {
	return errors.New("WindowsAgent release installation requires Windows")
}
