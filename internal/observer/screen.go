package observer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/observationapi"
)

type processIdentityResolver func(uint32, string) (ProcessIdentity, error)

type ScreenBackend struct {
	capturer  capture.RegionCapturer
	process   ProcessIdentity
	maxPixels uint64
	resolve   processIdentityResolver
}

func NewScreenBackend(capturer capture.RegionCapturer, process ProcessIdentity, maxPixels uint64) (*ScreenBackend, error) {
	return newScreenBackend(capturer, process, maxPixels, ResolveProcessIdentity)
}

func newScreenBackend(
	capturer capture.RegionCapturer,
	process ProcessIdentity,
	maxPixels uint64,
	resolve processIdentityResolver,
) (*ScreenBackend, error) {
	if capturer == nil {
		return nil, errors.New("screen capturer is required")
	}
	if process.PID == 0 || process.CreationTimeWindows == 0 || !filepath.IsAbs(process.ImagePath) || process.ImageSHA256 == "" {
		return nil, errors.New("exact owning process identity is required")
	}
	if maxPixels == 0 || maxPixels > 65_536 {
		return nil, errors.New("screen maxPixels must be from 1 through 65536")
	}
	if resolve == nil {
		return nil, errors.New("process identity resolver is required")
	}
	return &ScreenBackend{capturer: capturer, process: process, maxPixels: maxPixels, resolve: resolve}, nil
}

func (b *ScreenBackend) Call(ctx context.Context, namespace, operation string, arguments map[string]any) (BackendResult, error) {
	if namespace != observationapi.NamespaceScreen || operation != "readRegion" {
		return BackendResult{}, fmt.Errorf("screen backend does not implement %s.%s", namespace, operation)
	}
	request, err := parseScreenRegion(arguments, b.maxPixels)
	if err != nil {
		return BackendResult{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "validating-arguments", namespace, operation, err,
		)
	}
	result, err := b.capturer.CaptureRegion(ctx, request)
	if err != nil {
		var captureFailure *capture.Error
		if errors.As(err, &captureFailure) && captureFailure.Code == "screen_pixel_limit_exceeded" {
			return BackendResult{}, observationapi.NewError(
				"LIMIT_EXCEEDED", "mapping-screen-region", namespace, operation, err,
			)
		}
		return BackendResult{}, observationapi.NewError(
			"SCREEN_CAPTURE_FAILED", "capturing-primary-monitor", namespace, operation, err,
		)
	}
	observed, err := b.resolve(result.Foreground.ProcessID, result.Foreground.ExecutablePath)
	if err != nil {
		return BackendResult{}, observationapi.NewError(
			"FOREGROUND_CHANGED", "resolving-captured-foreground", namespace, operation, err,
		)
	}
	if !sameProcessIdentity(observed, b.process) {
		return BackendResult{}, observationapi.NewError(
			"FOREGROUND_CHANGED", "validating-captured-foreground", namespace, operation,
			fmt.Errorf("captured foreground PID %d does not match owning PID %d", observed.PID, b.process.PID),
		)
	}
	if err := validateScreenRegionResult(result, request); err != nil {
		return BackendResult{}, observationapi.NewError(
			"SCREEN_CAPTURE_INVALID", "validating-captured-region", namespace, operation, err,
		)
	}
	return BackendResult{
		Value: map[string]any{
			"sampling": string(request.Sampling),
			"coordinateSpace": map[string]any{
				"width": capture.ReferenceWidth, "height": capture.ReferenceHeight,
				"fit": "centered-16:9",
			},
			"frame": map[string]any{
				"width": result.FrameWidth, "height": result.FrameHeight,
				"capturedAt": result.Foreground.ObservedAt,
				"foreground": map[string]any{
					"processId":      result.Foreground.ProcessID,
					"executableName": result.Foreground.ExecutableName,
				},
			},
			"viewport": map[string]any{
				"left": result.Viewport.Left, "top": result.Viewport.Top,
				"width": result.Viewport.Width, "height": result.Viewport.Height,
			},
			"region": map[string]any{
				"x": request.Region.X, "y": request.Region.Y,
				"w": request.Region.Width, "h": request.Region.Height,
			},
			"physicalRegion": map[string]any{
				"left": result.PhysicalRegion.Left, "top": result.PhysicalRegion.Top,
				"width": result.PhysicalRegion.Width, "height": result.PhysicalRegion.Height,
			},
			"image": map[string]any{
				"width": result.ImageWidth, "height": result.ImageHeight,
				"encoding": "rgb24-packed", "pixels": result.Pixels,
			},
		},
		ScreenPixelsRead: uint64(len(result.Pixels)),
	}, nil
}

func parseScreenRegion(arguments map[string]any, maxPixels uint64) (capture.RegionRequest, error) {
	required := []string{"x", "y", "w", "h", "sampling"}
	if len(arguments) != len(required) {
		return capture.RegionRequest{}, errors.New("readRegion requires exactly x, y, w, h, and sampling")
	}
	for _, name := range required {
		if _, exists := arguments[name]; !exists {
			return capture.RegionRequest{}, fmt.Errorf("readRegion is missing %s", name)
		}
	}
	x, err := nonNegativeInt64(arguments["x"], "x")
	if err != nil {
		return capture.RegionRequest{}, err
	}
	y, err := nonNegativeInt64(arguments["y"], "y")
	if err != nil {
		return capture.RegionRequest{}, err
	}
	width, err := positiveInt64(arguments["w"], "w")
	if err != nil {
		return capture.RegionRequest{}, err
	}
	height, err := positiveInt64(arguments["h"], "h")
	if err != nil {
		return capture.RegionRequest{}, err
	}
	sampling, ok := arguments["sampling"].(string)
	if !ok || (sampling != string(capture.SamplingReference) && sampling != string(capture.SamplingNative)) {
		return capture.RegionRequest{}, errors.New("sampling must equal reference or native")
	}
	request := capture.RegionRequest{
		Region:   capture.ReferenceRegion{X: int(x), Y: int(y), Width: int(width), Height: int(height)},
		Sampling: capture.Sampling(sampling), MaxPixels: maxPixels,
	}
	if err := request.Region.Validate(); err != nil {
		return capture.RegionRequest{}, err
	}
	if request.Sampling == capture.SamplingReference && uint64(width)*uint64(height) > maxPixels {
		return capture.RegionRequest{}, fmt.Errorf("readRegion requests %d reference pixels, limit is %d", uint64(width)*uint64(height), maxPixels)
	}
	return request, nil
}

func validateScreenRegionResult(result capture.RegionResult, request capture.RegionRequest) error {
	if result.FrameWidth <= 0 || result.FrameHeight <= 0 ||
		result.ImageWidth <= 0 || result.ImageHeight <= 0 {
		return errors.New("captured frame and image dimensions must be positive")
	}
	if len(result.Pixels) != result.ImageWidth*result.ImageHeight {
		return errors.New("captured pixel count does not match image dimensions")
	}
	if uint64(len(result.Pixels)) > request.MaxPixels {
		return errors.New("captured pixel count exceeds the authorized limit")
	}
	if request.Sampling == capture.SamplingReference &&
		(result.ImageWidth != request.Region.Width || result.ImageHeight != request.Region.Height) {
		return errors.New("reference sampling output does not match the requested reference dimensions")
	}
	if request.Sampling == capture.SamplingNative &&
		(result.ImageWidth != result.PhysicalRegion.Width || result.ImageHeight != result.PhysicalRegion.Height) {
		return errors.New("native sampling output does not match the physical region dimensions")
	}
	if result.PhysicalRegion.Left < 0 || result.PhysicalRegion.Top < 0 ||
		result.PhysicalRegion.Width <= 0 || result.PhysicalRegion.Height <= 0 ||
		result.PhysicalRegion.Left+result.PhysicalRegion.Width > result.FrameWidth ||
		result.PhysicalRegion.Top+result.PhysicalRegion.Height > result.FrameHeight {
		return errors.New("captured physical region is outside the frame")
	}
	wantViewport, wantPhysical, err := capture.MapReferenceRegion(result.FrameWidth, result.FrameHeight, request.Region)
	if err != nil {
		return fmt.Errorf("remap captured reference region: %w", err)
	}
	if result.Viewport != wantViewport || result.PhysicalRegion != wantPhysical {
		return errors.New("captured viewport or physical region does not match the reference mapping")
	}
	return nil
}

func sameProcessIdentity(left, right ProcessIdentity) bool {
	return left.PID == right.PID &&
		left.CreationTimeWindows == right.CreationTimeWindows &&
		strings.EqualFold(left.ImagePath, right.ImagePath) &&
		strings.EqualFold(left.ImageSHA256, right.ImageSHA256)
}
