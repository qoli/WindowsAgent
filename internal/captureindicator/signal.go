// Package captureindicator exposes the session-local recent-capture signal
// published by the Capture Agent and observed by the display-only OSD. The
// signal carries no frame, request data, capture control, or success claim.
package captureindicator

import (
	"errors"
	"time"
)

const (
	SignalName    = `Local\WindowsAgent.Capture.Recent.v1`
	PulseDuration = 500 * time.Millisecond
)

// Notifier marks that one full or region capture request was accepted by the
// Capture Agent. A notification failure never changes capture execution.
type Notifier interface {
	Pulse() error
}

type unavailableNotifier struct{ cause error }

// Unavailable returns an explicitly disabled notification adapter. It does not
// substitute another signal source or change capture execution.
func Unavailable(cause error) Notifier {
	if cause == nil {
		cause = errors.New("capture indicator is unavailable")
	}
	return unavailableNotifier{cause: cause}
}

func (n unavailableNotifier) Pulse() error { return n.cause }
