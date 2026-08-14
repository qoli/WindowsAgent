package ocraction

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
)

var routeIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var actionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`)
var decisionPathPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*(\.[a-zA-Z][a-zA-Z0-9]*)+$`)

type RGBImage struct {
	Width  int
	Height int
	RGB    []byte
}

type GateEvidence struct {
	Accepted                       bool    `json:"accepted"`
	OrangeThreshold                int     `json:"orangeThreshold"`
	CenterOrangeRatio              float64 `json:"centerOrangeRatio"`
	MinimumCenterOrangeRatio       float64 `json:"minimumCenterOrangeRatio"`
	LowOrangeThreshold             int     `json:"lowOrangeThreshold"`
	ActiveOrangeColumnRatio        float64 `json:"activeOrangeColumnRatio"`
	MaximumActiveOrangeColumnRatio float64 `json:"maximumActiveOrangeColumnRatio"`
	HorizontalEdgeThreshold        int     `json:"horizontalEdgeThreshold"`
	HorizontalEdgeRatio            float64 `json:"horizontalEdgeRatio"`
	MinimumHorizontalEdgeRatio     float64 `json:"minimumHorizontalEdgeRatio"`
}

func (c CascadeConfig) Validate() error {
	if c.NativeCaptureMaxPixels == 0 || c.NativeCaptureMaxPixels > 262_144 {
		return errors.New("nativeCaptureMaxPixels must be from 1 through 262144")
	}
	if err := c.Primary.Validate("primary"); err != nil {
		return err
	}
	if err := c.Recovery.Validate("recovery"); err != nil {
		return err
	}
	if err := c.Validator.Route.Validate("validator.route"); err != nil {
		return err
	}
	ids := []string{c.Primary.ID, c.Recovery.ID, c.Validator.Route.ID}
	sort.Strings(ids)
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			return fmt.Errorf("route id %q must be unique", ids[index])
		}
	}
	if err := c.Gate.Validate(); err != nil {
		return err
	}
	if !actionIDPattern.MatchString(c.DecisionActionID) {
		return errors.New("decisionActionId must be one canonical Action id")
	}
	if !decisionPathPattern.MatchString(c.DecisionAcceptedPath) || !decisionPathPattern.MatchString(c.DecisionStatePath) {
		return errors.New("decisionAcceptedPath and decisionStatePath must be canonical dotted object paths")
	}
	if !routeIDPattern.MatchString(c.UnknownState) {
		return errors.New("unknownState must be a canonical state identifier")
	}
	if len(c.RecoveryAllowedStates) == 0 {
		return errors.New("recoveryAllowedStates must not be empty")
	}
	allowed := map[string]bool{}
	for _, state := range c.RecoveryAllowedStates {
		if !routeIDPattern.MatchString(state) {
			return fmt.Errorf("recoveryAllowedStates contains non-canonical state %q", state)
		}
		if allowed[state] {
			return fmt.Errorf("recoveryAllowedStates contains duplicate state %q", state)
		}
		allowed[state] = true
	}
	if allowed[c.UnknownState] {
		return errors.New("unknownState must not be a recoveryAllowedState")
	}
	if !routeIDPattern.MatchString(c.Validator.TriggerState) || !routeIDPattern.MatchString(c.Validator.RequiredState) {
		return errors.New("validator triggerState and requiredState must be canonical state identifiers")
	}
	if !allowed[c.Validator.TriggerState] {
		return errors.New("validator triggerState must be a recoveryAllowedState")
	}
	return nil
}

func (r RouteConfig) Validate(label string) error {
	if !routeIDPattern.MatchString(r.ID) {
		return fmt.Errorf("%s id must be a canonical uppercase identifier", label)
	}
	if r.TopPermille < 0 || r.BottomPermille > 1000 || r.TopPermille >= r.BottomPermille {
		return fmt.Errorf("%s crop must satisfy 0 <= topPermille < bottomPermille <= 1000", label)
	}
	return nil
}

func (g GateConfig) Validate() error {
	if g.TopPermille < 0 || g.BottomPermille > 1000 || g.TopPermille >= g.BottomPermille {
		return errors.New("gate crop must satisfy 0 <= topPermille < bottomPermille <= 1000")
	}
	if g.CenterLeftPermille < 0 || g.CenterRightPermille > 1000 || g.CenterLeftPermille >= g.CenterRightPermille {
		return errors.New("gate center band must satisfy 0 <= centerLeftPermille < centerRightPermille <= 1000")
	}
	for label, value := range map[string]int{
		"orangeThreshold": g.OrangeThreshold, "lowOrangeThreshold": g.LowOrangeThreshold,
		"horizontalEdgeThreshold": g.HorizontalEdgeThreshold,
	} {
		if value < 0 || value > 255 {
			return fmt.Errorf("gate %s must be from 0 through 255", label)
		}
	}
	for label, value := range map[string]float64{
		"minimumCenterOrangeRatio":       g.MinimumCenterOrangeRatio,
		"activeColumnPixelRatio":         g.ActiveColumnPixelRatio,
		"maximumActiveOrangeColumnRatio": g.MaximumActiveOrangeColumnRatio,
		"minimumHorizontalEdgeRatio":     g.MinimumHorizontalEdgeRatio,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("gate %s must be from 0 through 1", label)
		}
	}
	return nil
}

func ImageFromPixels(pixels []uint32, width, height int) (RGBImage, error) {
	if width <= 0 || height <= 0 || len(pixels) != width*height {
		return RGBImage{}, errors.New("RGB image source dimensions are invalid")
	}
	rgb := make([]byte, len(pixels)*3)
	for index, pixel := range pixels {
		rgb[index*3] = byte(pixel >> 16)
		rgb[index*3+1] = byte(pixel >> 8)
		rgb[index*3+2] = byte(pixel)
	}
	return RGBImage{Width: width, Height: height, RGB: rgb}, nil
}

func ResizeHalfPixel(source RGBImage, width, height int) (RGBImage, error) {
	if source.Width <= 0 || source.Height <= 0 || len(source.RGB) != source.Width*source.Height*3 || width <= 0 || height <= 0 {
		return RGBImage{}, errors.New("resize source and destination dimensions must be valid")
	}
	output := make([]byte, width*height*3)
	for outputY := 0; outputY < height; outputY++ {
		sourceY := (float64(outputY)+0.5)*float64(source.Height)/float64(height) - 0.5
		yFloor := math.Floor(sourceY)
		y0 := clamp(int(yFloor), 0, source.Height-1)
		y1 := clamp(int(yFloor)+1, 0, source.Height-1)
		weightY := sourceY - yFloor
		for outputX := 0; outputX < width; outputX++ {
			sourceX := (float64(outputX)+0.5)*float64(source.Width)/float64(width) - 0.5
			xFloor := math.Floor(sourceX)
			x0 := clamp(int(xFloor), 0, source.Width-1)
			x1 := clamp(int(xFloor)+1, 0, source.Width-1)
			weightX := sourceX - xFloor
			for channel := 0; channel < 3; channel++ {
				top := float64(source.RGB[(y0*source.Width+x0)*3+channel])*(1-weightX) + float64(source.RGB[(y0*source.Width+x1)*3+channel])*weightX
				bottom := float64(source.RGB[(y1*source.Width+x0)*3+channel])*(1-weightX) + float64(source.RGB[(y1*source.Width+x1)*3+channel])*weightX
				value := top*(1-weightY) + bottom*weightY
				// Match the pinned offline preprocessing contract: NumPy's
				// float-to-uint8 conversion truncates after clamping.
				output[(outputY*width+outputX)*3+channel] = byte(math.Max(0, math.Min(255, value)))
			}
		}
	}
	return RGBImage{Width: width, Height: height, RGB: output}, nil
}

func (image RGBImage) Crop(route RouteConfig) (RGBImage, error) {
	if err := route.Validate("route"); err != nil {
		return RGBImage{}, err
	}
	top := int(math.RoundToEven(float64(image.Height) * float64(route.TopPermille) / 1000))
	bottom := int(math.RoundToEven(float64(image.Height) * float64(route.BottomPermille) / 1000))
	if top < 0 || bottom > image.Height || top >= bottom || image.Width <= 0 || len(image.RGB) != image.Width*image.Height*3 {
		return RGBImage{}, errors.New("route crop does not produce a valid RGB image")
	}
	rgb := make([]byte, image.Width*(bottom-top)*3)
	copy(rgb, image.RGB[top*image.Width*3:bottom*image.Width*3])
	return RGBImage{Width: image.Width, Height: bottom - top, RGB: rgb}, nil
}

func EvaluateGate(image RGBImage, config GateConfig) (GateEvidence, error) {
	if err := config.Validate(); err != nil {
		return GateEvidence{}, err
	}
	gateImage, err := image.Crop(RouteConfig{ID: "GATE", TopPermille: config.TopPermille, BottomPermille: config.BottomPermille})
	if err != nil {
		return GateEvidence{}, err
	}
	if gateImage.Width < 2 || gateImage.Height < 1 {
		return GateEvidence{}, errors.New("gate image must be at least 2x1 pixels")
	}
	// The reviewed gate uses width//4 and 3*width//4, so declared horizontal
	// fractions use integer floor rather than nearest rounding.
	centerLeft := gateImage.Width * config.CenterLeftPermille / 1000
	centerRight := gateImage.Width * config.CenterRightPermille / 1000
	if centerLeft >= centerRight {
		return GateEvidence{}, errors.New("gate center band is empty")
	}
	centerOrange := 0
	activeColumns := 0
	edges := 0
	for x := 0; x < gateImage.Width; x++ {
		lowOrange := 0
		for y := 0; y < gateImage.Height; y++ {
			index := (y*gateImage.Width + x) * 3
			red, green, blue := int(gateImage.RGB[index]), int(gateImage.RGB[index+1]), int(gateImage.RGB[index+2])
			orange := min(red-green, green-blue)
			if x >= centerLeft && x < centerRight && orange > config.OrangeThreshold {
				centerOrange++
			}
			if orange > config.LowOrangeThreshold {
				lowOrange++
			}
			if x+1 < gateImage.Width {
				next := index + 3
				gray := 0.299*float64(red) + 0.587*float64(green) + 0.114*float64(blue)
				nextGray := 0.299*float64(gateImage.RGB[next]) + 0.587*float64(gateImage.RGB[next+1]) + 0.114*float64(gateImage.RGB[next+2])
				if math.Abs(nextGray-gray) > float64(config.HorizontalEdgeThreshold) {
					edges++
				}
			}
		}
		if float64(lowOrange)/float64(gateImage.Height) > config.ActiveColumnPixelRatio {
			activeColumns++
		}
	}
	centerRatio := float64(centerOrange) / float64((centerRight-centerLeft)*gateImage.Height)
	activeRatio := float64(activeColumns) / float64(gateImage.Width)
	edgeRatio := float64(edges) / float64((gateImage.Width-1)*gateImage.Height)
	orangeShape := centerRatio >= config.MinimumCenterOrangeRatio && activeRatio <= config.MaximumActiveOrangeColumnRatio
	evidence := GateEvidence{
		Accepted:        orangeShape || edgeRatio >= config.MinimumHorizontalEdgeRatio,
		OrangeThreshold: config.OrangeThreshold, CenterOrangeRatio: centerRatio,
		MinimumCenterOrangeRatio: config.MinimumCenterOrangeRatio,
		LowOrangeThreshold:       config.LowOrangeThreshold, ActiveOrangeColumnRatio: activeRatio,
		MaximumActiveOrangeColumnRatio: config.MaximumActiveOrangeColumnRatio,
		HorizontalEdgeThreshold:        config.HorizontalEdgeThreshold, HorizontalEdgeRatio: edgeRatio,
		MinimumHorizontalEdgeRatio: config.MinimumHorizontalEdgeRatio,
	}
	return evidence, nil
}

func clamp(value, lower, upper int) int {
	return min(max(value, lower), upper)
}
