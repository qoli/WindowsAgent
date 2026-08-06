package observer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/observationapi"
)

type screenFixtureCapturer struct {
	result capture.Result
	calls  int
	cursor bool
}

func (c *screenFixtureCapturer) Status(context.Context) (capture.Status, error) {
	return capture.Status{}, errors.New("unexpected status call")
}

func (c *screenFixtureCapturer) Capture(_ context.Context, cursor bool) (capture.Result, error) {
	c.calls++
	c.cursor = cursor
	return c.result, nil
}

func screenFixture(t *testing.T) (*ScreenBackend, *screenFixtureCapturer, ProcessIdentity) {
	t.Helper()
	imagePath := "/games/EliteDangerous64.exe"
	process := ProcessIdentity{
		PID: 7, CreationTimeWindows: 42, ImagePath: imagePath,
		ImageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			frame.SetNRGBA(x, y, color.NRGBA{R: uint8(10 + x), G: uint8(20 + y), B: uint8(30 + x + y), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	capturer := &screenFixtureCapturer{result: capture.Result{
		PNG: encoded.Bytes(), Width: 4, Height: 3,
		Foreground: foreground.Info{
			ObservedAt: time.Date(2026, 8, 7, 1, 2, 3, 0, time.UTC),
			ProcessID:  7, ExecutableName: "EliteDangerous64.exe", ExecutablePath: imagePath,
		},
	}}
	backend, err := newScreenBackend(capturer, process, 4, func(uint32, string) (ProcessIdentity, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend, capturer, process
}

func TestScreenBackendReadsBoundedRegionWithoutCursor(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	result, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"expectedWidth": 4, "expectedHeight": 3,
		"left": 1, "top": 1, "width": 2, "height": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturer.calls != 1 || capturer.cursor {
		t.Fatalf("capture calls = %d, cursor = %t", capturer.calls, capturer.cursor)
	}
	if result.ScreenPixelsRead != 4 {
		t.Fatalf("screen pixels = %d", result.ScreenPixelsRead)
	}
	value := result.Value.(map[string]any)
	pixels := value["pixels"].([]uint32)
	want := []uint32{11<<16 | 21<<8 | 32, 12<<16 | 21<<8 | 33, 11<<16 | 22<<8 | 33, 12<<16 | 22<<8 | 34}
	for index := range want {
		if pixels[index] != want[index] {
			t.Fatalf("pixel %d = %#06x, want %#06x", index, pixels[index], want[index])
		}
	}
}

func TestScreenBackendRejectsInvalidRegionBeforeCapture(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"expectedWidth": 4, "expectedHeight": 3,
		"left": 3, "top": 1, "width": 2, "height": 2,
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "OBSERVER_PROTOCOL_INVALID" {
		t.Fatalf("error = %#v", err)
	}
	if capturer.calls != 0 {
		t.Fatalf("invalid region captured %d frames", capturer.calls)
	}
}

func TestScreenBackendRejectsFrameProfileMismatch(t *testing.T) {
	backend, _, _ := screenFixture(t)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"expectedWidth": 5, "expectedHeight": 3,
		"left": 1, "top": 1, "width": 2, "height": 2,
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "SCREEN_PROFILE_MISMATCH" {
		t.Fatalf("error = %#v", err)
	}
}

func TestScreenBackendRejectsForegroundIdentityDrift(t *testing.T) {
	_, capturer, process := screenFixture(t)
	drifted := process
	drifted.CreationTimeWindows++
	backend, err := newScreenBackend(capturer, process, 4, func(uint32, string) (ProcessIdentity, error) {
		return drifted, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"expectedWidth": 4, "expectedHeight": 3,
		"left": 1, "top": 1, "width": 2, "height": 2,
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "FOREGROUND_CHANGED" {
		t.Fatalf("error = %#v", err)
	}
}
