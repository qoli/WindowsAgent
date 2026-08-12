package evidence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qoli/WindowsAgent/internal/videocapture"
)

type SampleSink interface {
	Append(context.Context, videocapture.Sample) (Record, error)
	Close(context.Context) error
}

type FramePublisher interface {
	Publish(context.Context, videocapture.Frame) error
}

type Recorder struct {
	Config      Config
	Stream      videocapture.Stream
	Lifecycle   videocapture.Lifecycle
	Sink        SampleSink
	FrameTap    FramePublisher
	OnCommitted func(Record)
	OnTapFailed func(error)
}

func (r Recorder) Validate() error {
	if r.Stream == nil || r.Lifecycle == nil || r.Sink == nil || r.FrameTap == nil {
		return errors.New("evidence video recorder dependencies are required")
	}
	if err := r.Config.Validate(); err != nil {
		return err
	}
	return nil
}

func (r Recorder) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("evidence video recorder context is required")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	streamErr := r.Stream.Run(ctx, time.Second, r.Lifecycle, func(sampleContext context.Context, sample videocapture.Sample) error {
		record, err := r.Sink.Append(sampleContext, sample)
		if err != nil {
			return fmt.Errorf("commit evidence video sample: %w", err)
		}
		if r.OnCommitted != nil {
			r.OnCommitted(record)
		}
		if sample.Frame != nil && record.Kind == "frame" {
			if err := r.FrameTap.Publish(sampleContext, *sample.Frame); err != nil && r.OnTapFailed != nil {
				r.OnTapFailed(err)
			}
		}
		return nil
	})
	closeContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	closeErr := r.Sink.Close(closeContext)
	if streamErr != nil {
		if closeErr != nil {
			return fmt.Errorf("record evidence video: %v; finalize segment: %w", streamErr, closeErr)
		}
		return streamErr
	}
	return closeErr
}
