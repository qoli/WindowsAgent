package observer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/observationapi"
)

type processIdentityResolver func(uint32, string) (ProcessIdentity, error)

type ScreenBackend struct {
	capturer  capture.Capturer
	process   ProcessIdentity
	maxPixels uint64
	resolve   processIdentityResolver
}

func NewScreenBackend(capturer capture.Capturer, process ProcessIdentity, maxPixels uint64) (*ScreenBackend, error) {
	return newScreenBackend(capturer, process, maxPixels, ResolveProcessIdentity)
}

func newScreenBackend(
	capturer capture.Capturer,
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
	region, err := parseScreenRegion(arguments, b.maxPixels)
	if err != nil {
		return BackendResult{}, observationapi.NewError(
			"OBSERVER_PROTOCOL_INVALID", "validating-arguments", namespace, operation, err,
		)
	}
	result, err := b.capturer.Capture(ctx, false)
	if err != nil {
		return BackendResult{}, observationapi.NewError(
			"SCREEN_CAPTURE_FAILED", "capturing-primary-monitor", namespace, operation, err,
		)
	}
	if result.Width != region.expectedWidth || result.Height != region.expectedHeight {
		return BackendResult{}, observationapi.NewError(
			"SCREEN_PROFILE_MISMATCH", "validating-frame-profile", namespace, operation,
			fmt.Errorf("captured frame is %dx%d, expected %dx%d", result.Width, result.Height, region.expectedWidth, region.expectedHeight),
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
	decoded, err := png.Decode(bytes.NewReader(result.PNG))
	if err != nil {
		return BackendResult{}, observationapi.NewError(
			"SCREEN_CAPTURE_INVALID", "decoding-captured-frame", namespace, operation, err,
		)
	}
	if decoded.Bounds().Dx() != result.Width || decoded.Bounds().Dy() != result.Height {
		return BackendResult{}, observationapi.NewError(
			"SCREEN_CAPTURE_INVALID", "validating-captured-frame", namespace, operation,
			errors.New("captured PNG dimensions do not match capture metadata"),
		)
	}
	pixels := make([]uint32, 0, region.width*region.height)
	for y := region.top; y < region.top+region.height; y++ {
		for x := region.left; x < region.left+region.width; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(decoded.Bounds().Min.X+x, decoded.Bounds().Min.Y+y)).(color.NRGBA)
			pixels = append(pixels, uint32(pixel.R)<<16|uint32(pixel.G)<<8|uint32(pixel.B))
		}
	}
	return BackendResult{
		Value: map[string]any{
			"frame": map[string]any{
				"width": result.Width, "height": result.Height,
				"capturedAt": result.Foreground.ObservedAt,
				"foreground": map[string]any{
					"processId":      result.Foreground.ProcessID,
					"executableName": result.Foreground.ExecutableName,
				},
			},
			"region": map[string]any{
				"left": region.left, "top": region.top, "width": region.width, "height": region.height,
			},
			"encoding": "rgb24-packed",
			"pixels":   pixels,
		},
		ScreenPixelsRead: uint64(len(pixels)),
	}, nil
}

type screenRegion struct {
	expectedWidth  int
	expectedHeight int
	left           int
	top            int
	width          int
	height         int
}

func parseScreenRegion(arguments map[string]any, maxPixels uint64) (screenRegion, error) {
	required := []string{"expectedWidth", "expectedHeight", "left", "top", "width", "height"}
	if len(arguments) != len(required) {
		return screenRegion{}, errors.New("readRegion requires exactly expectedWidth, expectedHeight, left, top, width, and height")
	}
	for _, name := range required {
		if _, exists := arguments[name]; !exists {
			return screenRegion{}, fmt.Errorf("readRegion is missing %s", name)
		}
	}
	expectedWidth, err := positiveInt64(arguments["expectedWidth"], "expectedWidth")
	if err != nil {
		return screenRegion{}, err
	}
	expectedHeight, err := positiveInt64(arguments["expectedHeight"], "expectedHeight")
	if err != nil {
		return screenRegion{}, err
	}
	left, err := nonNegativeInt64(arguments["left"], "left")
	if err != nil {
		return screenRegion{}, err
	}
	top, err := nonNegativeInt64(arguments["top"], "top")
	if err != nil {
		return screenRegion{}, err
	}
	width, err := positiveInt64(arguments["width"], "width")
	if err != nil {
		return screenRegion{}, err
	}
	height, err := positiveInt64(arguments["height"], "height")
	if err != nil {
		return screenRegion{}, err
	}
	if expectedWidth > 16_384 || expectedHeight > 16_384 || left+width > expectedWidth || top+height > expectedHeight {
		return screenRegion{}, errors.New("readRegion rectangle must be within the expected frame profile")
	}
	pixelCount := uint64(width) * uint64(height)
	if pixelCount > maxPixels {
		return screenRegion{}, fmt.Errorf("readRegion requests %d pixels, limit is %d", pixelCount, maxPixels)
	}
	return screenRegion{
		expectedWidth: int(expectedWidth), expectedHeight: int(expectedHeight),
		left: int(left), top: int(top), width: int(width), height: int(height),
	}, nil
}

func sameProcessIdentity(left, right ProcessIdentity) bool {
	return left.PID == right.PID &&
		left.CreationTimeWindows == right.CreationTimeWindows &&
		strings.EqualFold(left.ImagePath, right.ImagePath) &&
		strings.EqualFold(left.ImageSHA256, right.ImageSHA256)
}
