// Package frametap exposes the newest Evidence frame to PC-local consumers
// through a read-only shared-memory view. It is an index aid, not evidence
// storage, and it never supplies an older frame when the newest frame is stale.
package frametap

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

const (
	Width        = 1920
	Height       = 1080
	HeaderBytes  = 4096
	PixelBytes   = Width * Height * 4
	MappingBytes = HeaderBytes + PixelBytes
)

var (
	ErrNoFrame    = errors.New("frame tap has no published frame")
	ErrNoNewFrame = errors.New("frame tap has no frame newer than cursor")
)

type Publisher interface {
	Publish(context.Context, videocapture.Frame) error
	Close() error
}

type Reader interface {
	Latest(context.Context, time.Time) (videocapture.Frame, error)
	Close() error
}

func ValidateName(name string) error {
	const prefix = `Local\WindowsAgent.Evidence.`
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".v1") || len(name) > 180 {
		return errors.New("frame tap name must be a bounded Local\\WindowsAgent.Evidence.*.v1 mapping")
	}
	for _, character := range name[len(prefix):] {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '-' || character == '_' {
			continue
		}
		return errors.New("frame tap name must contain only ASCII letters, digits, dot, dash, or underscore after its prefix")
	}
	return nil
}

func publisherOwnershipName(name string) string {
	return name + ".Publisher"
}
