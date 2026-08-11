package evidence

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Frame struct {
	CaptureID   string
	ObservedAt  time.Time
	ContentType string
	Content     []byte
	Width       int
	Height      int
	SHA256      string
}

type CaptureSource interface {
	Capture(context.Context) (Frame, error)
}

type Sink interface {
	CommitFrame(context.Context, time.Time, Frame) (Record, error)
	CommitGap(context.Context, time.Time, string, error) (Record, error)
}

type Recorder struct {
	Config      Config
	Capture     CaptureSource
	Sink        Sink
	OnCommitted func(Record)
}

func (r Recorder) Run(ctx context.Context) error {
	if ctx == nil || r.Capture == nil || r.Sink == nil {
		return errors.New("evidence recorder dependencies are required")
	}
	if err := r.Config.Validate(); err != nil {
		return err
	}
	next := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	for {
		delay := time.Until(next)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
		if err := r.recordSlot(ctx, next); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		next = next.Add(time.Second)
		for slotMissed(next, time.Now().UTC()) {
			record, err := r.Sink.CommitGap(ctx, next, "scheduler_overrun", errors.New("capture did not finish before this one-second slot"))
			if err != nil {
				return fmt.Errorf("commit scheduler gap: %w", err)
			}
			r.notify(record)
			next = next.Add(time.Second)
		}
	}
}

func slotMissed(scheduled, now time.Time) bool {
	return !scheduled.Add(time.Second).After(now)
}

func (r Recorder) recordSlot(ctx context.Context, scheduled time.Time) error {
	slotContext, cancel := context.WithTimeout(ctx, r.Config.CaptureTimeout())
	frame, err := r.Capture.Capture(slotContext)
	cancel()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		record, commitErr := r.Sink.CommitGap(ctx, scheduled, "capture", err)
		if commitErr != nil {
			return fmt.Errorf("commit capture gap: %w", commitErr)
		}
		r.notify(record)
		return nil
	}
	record, err := r.Sink.CommitFrame(ctx, scheduled, frame)
	if err != nil {
		return fmt.Errorf("commit evidence frame: %w", err)
	}
	r.notify(record)
	return nil
}

func (r Recorder) notify(record Record) {
	if r.OnCommitted != nil {
		r.OnCommitted(record)
	}
}
