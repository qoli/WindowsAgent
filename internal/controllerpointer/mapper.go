package controllerpointer

import (
	"errors"
	"math"
	"time"
)

// Config declares the complete stick-to-pointer transfer function. Callers
// must choose every policy value explicitly; the mapper supplies no hidden
// dead zone, acceleration curve, or speed fallback.
type Config struct {
	DeadZone                float64
	CurveExponent           float64
	MaxSpeedPixelsPerSecond float64
	InvertY                 bool
}

func (c Config) Validate() error {
	if math.IsNaN(c.DeadZone) || math.IsInf(c.DeadZone, 0) || c.DeadZone < 0 || c.DeadZone >= 1 {
		return errors.New("controller pointer dead zone must be finite and in [0,1)")
	}
	if math.IsNaN(c.CurveExponent) || math.IsInf(c.CurveExponent, 0) || c.CurveExponent <= 0 {
		return errors.New("controller pointer curve exponent must be finite and positive")
	}
	if math.IsNaN(c.MaxSpeedPixelsPerSecond) || math.IsInf(c.MaxSpeedPixelsPerSecond, 0) || c.MaxSpeedPixelsPerSecond <= 0 {
		return errors.New("controller pointer maximum speed must be finite and positive")
	}
	return nil
}

// Sample is one normalized stick sample. X and Y must each be in [-1,1].
type Sample struct {
	X float64
	Y float64
}

// Motion is the relative integer pointer movement for one sample interval.
// NormalizedX and NormalizedY expose the post-dead-zone, post-curve vector for
// evidence and tuning without exposing mapper-private fractional carry.
type Motion struct {
	DX          int
	DY          int
	NormalizedX float64
	NormalizedY float64
}

// Mapper owns radial dead-zone removal, the configured response curve, and
// fractional pixel carry across samples.
type Mapper struct {
	config Config
	carryX float64
	carryY float64
}

func NewMapper(config Config) (*Mapper, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Mapper{config: config}, nil
}

func (m *Mapper) Step(sample Sample, elapsed time.Duration) (Motion, error) {
	if m == nil {
		return Motion{}, errors.New("controller pointer mapper is required")
	}
	if elapsed <= 0 {
		return Motion{}, errors.New("controller pointer sample interval must be positive")
	}
	if err := validateSample(sample); err != nil {
		return Motion{}, err
	}

	x, y, magnitude := radialDeadZone(sample.X, sample.Y, m.config.DeadZone)
	if magnitude == 0 {
		m.carryX = 0
		m.carryY = 0
		return Motion{}, nil
	}
	curvedMagnitude := math.Pow(magnitude, m.config.CurveExponent)
	x = x / magnitude * curvedMagnitude
	y = y / magnitude * curvedMagnitude
	if m.config.InvertY {
		y = -y
	}

	seconds := elapsed.Seconds()
	m.carryX += x * m.config.MaxSpeedPixelsPerSecond * seconds
	m.carryY += y * m.config.MaxSpeedPixelsPerSecond * seconds
	dx := int(math.Round(m.carryX))
	dy := int(math.Round(m.carryY))
	m.carryX -= float64(dx)
	m.carryY -= float64(dy)
	return Motion{DX: dx, DY: dy, NormalizedX: x, NormalizedY: y}, nil
}

func validateSample(sample Sample) error {
	if math.IsNaN(sample.X) || math.IsInf(sample.X, 0) || sample.X < -1 || sample.X > 1 ||
		math.IsNaN(sample.Y) || math.IsInf(sample.Y, 0) || sample.Y < -1 || sample.Y > 1 {
		return errors.New("controller pointer sample axes must be finite and in [-1,1]")
	}
	return nil
}

func radialDeadZone(x, y, deadZone float64) (float64, float64, float64) {
	magnitude := math.Hypot(x, y)
	if magnitude <= deadZone {
		return 0, 0, 0
	}
	if magnitude > 1 {
		magnitude = 1
	}
	scaled := (magnitude - deadZone) / (1 - deadZone)
	return x / math.Hypot(x, y) * scaled, y / math.Hypot(x, y) * scaled, scaled
}
