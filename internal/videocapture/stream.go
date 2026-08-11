// Package videocapture defines the persistent sampled-frame stream seam used
// by PC-side video recorders. It does not define request-driven screenshots.
package videocapture

import (
	"context"
	"errors"
	"time"
)

const PixelFormatBGRX32BottomUp = "bgrx32-bottom-up"

type Frame struct {
	Sequence             uint64
	ScheduledAt          time.Time
	ObservedAt           time.Time
	ForegroundExecutable string
	Width                int
	Height               int
	PixelFormat          string
	Pixels               []byte
}

func (f Frame) Validate() error {
	if f.Sequence == 0 || f.ScheduledAt.IsZero() || f.ScheduledAt.Location() != time.UTC || f.ScheduledAt.Nanosecond() != 0 {
		return errors.New("video frame identity or UTC slot is invalid")
	}
	if f.ObservedAt.IsZero() || f.ForegroundExecutable == "" {
		return errors.New("video frame observation provenance is required")
	}
	if f.Width != 1920 || f.Height != 1080 || f.PixelFormat != PixelFormatBGRX32BottomUp {
		return errors.New("video frame must be 1920x1080 BGRX32 bottom-up")
	}
	if len(f.Pixels) != f.Width*f.Height*4 {
		return errors.New("video frame pixel length is invalid")
	}
	return nil
}

type Sample struct {
	ScheduledAt time.Time
	Frame       *Frame
	Stage       string
	Err         error
}

func (s Sample) Validate() error {
	if s.ScheduledAt.IsZero() || s.ScheduledAt.Location() != time.UTC || s.ScheduledAt.Nanosecond() != 0 {
		return errors.New("video sample UTC slot is invalid")
	}
	if s.Frame != nil {
		if s.Stage != "" || s.Err != nil || !s.Frame.ScheduledAt.Equal(s.ScheduledAt) {
			return errors.New("video frame sample is internally inconsistent")
		}
		return s.Frame.Validate()
	}
	if s.Stage == "" || s.Err == nil {
		return errors.New("video gap sample requires a stage and error")
	}
	return nil
}

type Consumer func(context.Context, Sample) error

type Stream interface {
	Run(context.Context, time.Duration, Consumer) error
}
