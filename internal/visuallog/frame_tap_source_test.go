package visuallog

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/frametap"
	"github.com/qoli/WindowsAgent/internal/videocapture"
)

type fakeTapReader struct {
	frame videocapture.Frame
	err   error
}

func (f fakeTapReader) Latest(context.Context, time.Time) (videocapture.Frame, error) {
	return f.frame, f.err
}
func (fakeTapReader) Close() error { return nil }

func TestFrameTapSourceEncodesCurrentFrameWithoutHTTP(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	pixels := make([]byte, 1920*1080*4)
	for offset := 0; offset < len(pixels); offset += 4 {
		pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = 30, 20, 10, 255
	}
	reader := fakeTapReader{frame: videocapture.Frame{Sequence: 7, ScheduledAt: at, ObservedAt: at, ForegroundExecutable: "Game.exe", Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: pixels}}
	source, err := NewFrameTapSource(reader, "Game.exe", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return at.Add(time.Second) }
	frame, err := source.Latest(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.DecodeConfig(bytes.NewReader(frame.Content))
	if err != nil || decoded.Width != 1920 || decoded.Height != 1080 || frame.ForegroundRevision != 7 {
		t.Fatalf("frame=%+v jpeg=%+v err=%v", frame, decoded, err)
	}
}

func TestFrameTapSourcePreservesNoNewFrame(t *testing.T) {
	source, _ := NewFrameTapSource(fakeTapReader{err: frametap.ErrNoNewFrame}, "Game.exe", 3*time.Second)
	if _, err := source.Latest(context.Background(), time.Time{}); !errors.Is(err, ErrNoNewEvidenceFrame) {
		t.Fatalf("error=%v", err)
	}
}
