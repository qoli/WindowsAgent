//go:build !windows

package assisttailscale

import (
	"context"

	"github.com/qoli/WindowsAgent/internal/assistgui"
)

func start(context.Context, string, []byte) (assistgui.TailscaleSnapshot, error) {
	return assistgui.TailscaleSnapshot{}, ErrUnsupported
}

func stop(context.Context, string) (assistgui.TailscaleSnapshot, error) {
	return assistgui.TailscaleSnapshot{}, ErrUnsupported
}
