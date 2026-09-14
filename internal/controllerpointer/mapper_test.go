package controllerpointer

import (
	"math"
	"testing"
	"time"
)

func TestMapperRejectsInvalidConfiguration(t *testing.T) {
	tests := []Config{
		{DeadZone: -0.1, CurveExponent: 1, MaxSpeedPixelsPerSecond: 1},
		{DeadZone: 1, CurveExponent: 1, MaxSpeedPixelsPerSecond: 1},
		{DeadZone: 0, CurveExponent: 0, MaxSpeedPixelsPerSecond: 1},
		{DeadZone: 0, CurveExponent: 1, MaxSpeedPixelsPerSecond: 0},
	}
	for _, config := range tests {
		if _, err := NewMapper(config); err == nil {
			t.Fatalf("NewMapper(%+v) succeeded", config)
		}
	}
}

func TestMapperAppliesRadialDeadZoneAndCurve(t *testing.T) {
	mapper, err := NewMapper(Config{DeadZone: 0.2, CurveExponent: 2, MaxSpeedPixelsPerSecond: 100})
	if err != nil {
		t.Fatal(err)
	}
	inside, err := mapper.Step(Sample{X: 0.12, Y: 0.12}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if inside != (Motion{}) {
		t.Fatalf("inside dead zone motion=%+v", inside)
	}

	motion, err := mapper.Step(Sample{X: 0.6}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if motion.DX != 25 || motion.DY != 0 || math.Abs(motion.NormalizedX-0.25) > 1e-12 {
		t.Fatalf("motion=%+v", motion)
	}
}

func TestMapperPreservesDirectionAndCanInvertY(t *testing.T) {
	mapper, err := NewMapper(Config{DeadZone: 0, CurveExponent: 1, MaxSpeedPixelsPerSecond: 100, InvertY: true})
	if err != nil {
		t.Fatal(err)
	}
	motion, err := mapper.Step(Sample{X: 0.3, Y: 0.4}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if motion.DX != 30 || motion.DY != -40 || math.Abs(motion.NormalizedX-0.3) > 1e-12 || math.Abs(motion.NormalizedY+0.4) > 1e-12 {
		t.Fatalf("motion=%+v", motion)
	}
}

func TestMapperAccumulatesFractionalPixelsAndClearsCarryAtRest(t *testing.T) {
	mapper, err := NewMapper(Config{DeadZone: 0, CurveExponent: 1, MaxSpeedPixelsPerSecond: 10})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for index := 0; index < 4; index++ {
		motion, stepErr := mapper.Step(Sample{X: 0.25}, 100*time.Millisecond)
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		if index == 0 && motion.DX != 0 {
			t.Fatalf("step %d motion=%+v", index, motion)
		}
		total += motion.DX
	}
	if total != 1 {
		t.Fatalf("four-step total=%d", total)
	}

	if _, err := mapper.Step(Sample{}, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	afterRest, err := mapper.Step(Sample{X: 0.25}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if afterRest.DX != 0 {
		t.Fatalf("carry was not cleared at rest: %+v", afterRest)
	}
}

func TestMapperRejectsInvalidSampleAndInterval(t *testing.T) {
	mapper, err := NewMapper(Config{DeadZone: 0, CurveExponent: 1, MaxSpeedPixelsPerSecond: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mapper.Step(Sample{X: math.NaN()}, time.Second); err == nil {
		t.Fatal("NaN sample succeeded")
	}
	if _, err := mapper.Step(Sample{}, 0); err == nil {
		t.Fatal("zero interval succeeded")
	}
}
