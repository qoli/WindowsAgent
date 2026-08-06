package capture

import (
	"context"
	"errors"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/foreground"
)

const (
	ReferenceWidth  = 1920
	ReferenceHeight = 1080
)

type Sampling string

const (
	SamplingReference Sampling = "reference"
	SamplingNative    Sampling = "native"
)

type ReferenceRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"w"`
	Height int `json:"h"`
}

type PixelRegion struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type RegionRequest struct {
	Region    ReferenceRegion
	Sampling  Sampling
	MaxPixels uint64
}

type RegionResult struct {
	Pixels             []uint32
	ImageWidth         int
	ImageHeight        int
	FrameWidth         int
	FrameHeight        int
	Viewport           PixelRegion
	PhysicalRegion     PixelRegion
	Monitor            Monitor
	Foreground         foreground.Info
	CapturePixelFormat string
	ToneMapped         bool
}

type RegionCapturer interface {
	CaptureRegion(context.Context, RegionRequest) (RegionResult, error)
}

func (r ReferenceRegion) Validate() error {
	if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 {
		return errors.New("reference region coordinates must be non-negative and dimensions must be positive")
	}
	if r.X+r.Width > ReferenceWidth || r.Y+r.Height > ReferenceHeight {
		return fmt.Errorf("reference region must be within %dx%d", ReferenceWidth, ReferenceHeight)
	}
	return nil
}

func MapReferenceRegion(frameWidth, frameHeight int, region ReferenceRegion) (PixelRegion, PixelRegion, error) {
	if frameWidth <= 0 || frameHeight <= 0 {
		return PixelRegion{}, PixelRegion{}, errors.New("frame dimensions must be positive")
	}
	if err := region.Validate(); err != nil {
		return PixelRegion{}, PixelRegion{}, err
	}

	viewport := PixelRegion{Width: frameWidth, Height: frameHeight}
	if int64(frameWidth)*ReferenceHeight >= int64(frameHeight)*ReferenceWidth {
		viewport.Width = int(int64(frameHeight) * ReferenceWidth / ReferenceHeight)
		viewport.Left = (frameWidth - viewport.Width) / 2
	} else {
		viewport.Height = int(int64(frameWidth) * ReferenceHeight / ReferenceWidth)
		viewport.Top = (frameHeight - viewport.Height) / 2
	}

	left := viewport.Left + scaleFloor(region.X, viewport.Width, ReferenceWidth)
	top := viewport.Top + scaleFloor(region.Y, viewport.Height, ReferenceHeight)
	right := viewport.Left + scaleCeil(region.X+region.Width, viewport.Width, ReferenceWidth)
	bottom := viewport.Top + scaleCeil(region.Y+region.Height, viewport.Height, ReferenceHeight)
	physical := PixelRegion{Left: left, Top: top, Width: right - left, Height: bottom - top}
	if physical.Width <= 0 || physical.Height <= 0 ||
		physical.Left < 0 || physical.Top < 0 ||
		physical.Left+physical.Width > frameWidth || physical.Top+physical.Height > frameHeight {
		return PixelRegion{}, PixelRegion{}, errors.New("mapped physical region is outside the captured frame")
	}
	return viewport, physical, nil
}

func scaleFloor(value, physical, reference int) int {
	return int(int64(value) * int64(physical) / int64(reference))
}

func scaleCeil(value, physical, reference int) int {
	numerator := int64(value) * int64(physical)
	return int((numerator + int64(reference) - 1) / int64(reference))
}
