package visuallog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
	"time"

	"github.com/qoli/WindowsAgent/internal/frametap"
	"github.com/qoli/WindowsAgent/internal/videocapture"
)

const MaxFrameBytes = 32 << 20

var ErrNoNewEvidenceFrame = errors.New("no new evidence frame")

type Frame struct {
	ScheduledAt          time.Time
	CaptureID            string
	ObservedAt           time.Time
	ForegroundExecutable string
	ContentType          string
	Content              []byte
	ForegroundRevision   uint64
}

type FrameSource interface {
	Latest(context.Context, time.Time) (Frame, error)
}

type FrameTapSource struct {
	reader           frametap.Reader
	targetExecutable string
	maxFrameAge      time.Duration
	now              func() time.Time
}

func NewFrameTapSource(reader frametap.Reader, targetExecutable string, maxFrameAge time.Duration) (*FrameTapSource, error) {
	if reader == nil {
		return nil, errors.New("visual log frame tap reader is required")
	}
	if strings.TrimSpace(targetExecutable) != targetExecutable || strings.ContainsAny(targetExecutable, `/\`) || !strings.HasSuffix(strings.ToLower(targetExecutable), ".exe") {
		return nil, errors.New("visual log target executable is invalid")
	}
	if maxFrameAge <= 0 || maxFrameAge > time.Minute {
		return nil, errors.New("visual log max frame age must be greater than zero and at most one minute")
	}
	return &FrameTapSource{reader: reader, targetExecutable: targetExecutable, maxFrameAge: maxFrameAge, now: time.Now}, nil
}

func (s *FrameTapSource) Latest(ctx context.Context, after time.Time) (Frame, error) {
	if s == nil || ctx == nil {
		return Frame{}, errors.New("visual log frame tap source and context are required")
	}
	source, err := s.reader.Latest(ctx, after)
	if errors.Is(err, frametap.ErrNoFrame) || errors.Is(err, frametap.ErrNoNewFrame) {
		return Frame{}, ErrNoNewEvidenceFrame
	}
	if err != nil {
		return Frame{}, fmt.Errorf("read local evidence frame tap: %w", err)
	}
	if source.ForegroundExecutable != s.targetExecutable {
		return Frame{}, fmt.Errorf("frame tap foreground is %q, expected %q", source.ForegroundExecutable, s.targetExecutable)
	}
	age := s.now().UTC().Sub(source.ObservedAt.UTC())
	if age < 0 || age > s.maxFrameAge {
		return Frame{}, fmt.Errorf("frame tap age %s exceeds %s", age, s.maxFrameAge)
	}
	content, err := encodeJPEG(source)
	if err != nil {
		return Frame{}, err
	}
	return Frame{ScheduledAt: source.ScheduledAt, CaptureID: fmt.Sprintf("evidence-slot-%s-%d", source.ScheduledAt.Format("20060102T150405Z"), source.Sequence), ObservedAt: source.ObservedAt, ForegroundExecutable: source.ForegroundExecutable, ContentType: "image/jpeg", Content: content, ForegroundRevision: source.Sequence}, nil
}

func encodeJPEG(frame videocapture.Frame) ([]byte, error) {
	if err := frame.Validate(); err != nil {
		return nil, err
	}
	imageFrame := image.NewNRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	for y := 0; y < frame.Height; y++ {
		source := (frame.Height - 1 - y) * frame.Width * 4
		destination := y * imageFrame.Stride
		for x := 0; x < frame.Width; x++ {
			imageFrame.Pix[destination] = frame.Pixels[source+2]
			imageFrame.Pix[destination+1] = frame.Pixels[source+1]
			imageFrame.Pix[destination+2] = frame.Pixels[source]
			imageFrame.Pix[destination+3] = 0xff
			source += 4
			destination += 4
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, imageFrame, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode frame tap JPEG: %w", err)
	}
	if output.Len() < 1 || output.Len() > MaxFrameBytes {
		return nil, errors.New("frame tap JPEG size is invalid")
	}
	return output.Bytes(), nil
}
