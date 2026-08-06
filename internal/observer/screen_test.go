package observer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/observationapi"
)

type screenFixtureCapturer struct {
	result  capture.RegionResult
	err     error
	calls   int
	request capture.RegionRequest
}

func (c *screenFixtureCapturer) CaptureRegion(_ context.Context, request capture.RegionRequest) (capture.RegionResult, error) {
	c.calls++
	c.request = request
	return c.result, c.err
}

func screenFixture(t *testing.T) (*ScreenBackend, *screenFixtureCapturer, ProcessIdentity) {
	t.Helper()
	imagePath := "/games/EliteDangerous64.exe"
	process := ProcessIdentity{
		PID: 7, CreationTimeWindows: 42, ImagePath: imagePath,
		ImageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	capturer := &screenFixtureCapturer{result: capture.RegionResult{
		Pixels:     []uint32{0x0b1520, 0x0c1521, 0x0b1621, 0x0c1622},
		ImageWidth: 2, ImageHeight: 2, FrameWidth: 3840, FrameHeight: 2160,
		Viewport:       capture.PixelRegion{Width: 3840, Height: 2160},
		PhysicalRegion: capture.PixelRegion{Left: 20, Top: 40, Width: 4, Height: 4},
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

func TestScreenBackendReadsReferenceRegion(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	result, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"x": 10, "y": 20, "w": 2, "h": 2, "sampling": "reference",
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturer.calls != 1 {
		t.Fatalf("capture calls = %d", capturer.calls)
	}
	wantRequest := capture.RegionRequest{
		Region:   capture.ReferenceRegion{X: 10, Y: 20, Width: 2, Height: 2},
		Sampling: capture.SamplingReference, MaxPixels: 4,
	}
	if capturer.request != wantRequest {
		t.Fatalf("request = %#v, want %#v", capturer.request, wantRequest)
	}
	if result.ScreenPixelsRead != 4 {
		t.Fatalf("screen pixels = %d", result.ScreenPixelsRead)
	}
	value := result.Value.(map[string]any)
	if value["sampling"] != "reference" {
		t.Fatalf("sampling = %#v", value["sampling"])
	}
	image := value["image"].(map[string]any)
	if image["width"] != 2 || image["height"] != 2 || len(image["pixels"].([]uint32)) != 4 {
		t.Fatalf("image = %#v", image)
	}
}

func TestScreenBackendRejectsInvalidRegionBeforeCapture(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"x": 1919, "y": 20, "w": 2, "h": 2, "sampling": "reference",
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "OBSERVER_PROTOCOL_INVALID" {
		t.Fatalf("error = %#v", err)
	}
	if capturer.calls != 0 {
		t.Fatalf("invalid region captured %d frames", capturer.calls)
	}
}

func TestScreenBackendRejectsUnknownSamplingBeforeCapture(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"x": 10, "y": 20, "w": 2, "h": 2, "sampling": "automatic",
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "OBSERVER_PROTOCOL_INVALID" {
		t.Fatalf("error = %#v", err)
	}
	if capturer.calls != 0 {
		t.Fatalf("unknown sampling captured %d frames", capturer.calls)
	}
}

func TestScreenBackendRejectsLegacyProfileArguments(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"expectedWidth": 3840, "expectedHeight": 2160,
		"left": 20, "top": 40, "width": 4, "height": 4,
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "OBSERVER_PROTOCOL_INVALID" {
		t.Fatalf("error = %#v", err)
	}
	if capturer.calls != 0 {
		t.Fatalf("legacy arguments captured %d frames", capturer.calls)
	}
}

func TestScreenBackendReadsNativeRegion(t *testing.T) {
	_, capturer, process := screenFixture(t)
	capturer.result.Pixels = make([]uint32, 16)
	capturer.result.ImageWidth = 4
	capturer.result.ImageHeight = 4
	backend, err := newScreenBackend(capturer, process, 16, func(uint32, string) (ProcessIdentity, error) {
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"x": 10, "y": 20, "w": 2, "h": 2, "sampling": "native",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ScreenPixelsRead != 16 || capturer.request.Sampling != capture.SamplingNative {
		t.Fatalf("result = %#v, request = %#v", result, capturer.request)
	}
}

func TestScreenBackendRejectsMappedNativePixelLimit(t *testing.T) {
	backend, capturer, _ := screenFixture(t)
	capturer.err = capture.Failure("screen_pixel_limit_exceeded", "native region is too large", nil)
	_, err := backend.Call(context.Background(), "screen", "readRegion", map[string]any{
		"x": 10, "y": 20, "w": 2, "h": 2, "sampling": "native",
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "LIMIT_EXCEEDED" {
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
		"x": 10, "y": 20, "w": 2, "h": 2, "sampling": "reference",
	})
	var typed *observationapi.Error
	if !errors.As(err, &typed) || typed.Kind != "FOREGROUND_CHANGED" {
		t.Fatalf("error = %#v", err)
	}
}
