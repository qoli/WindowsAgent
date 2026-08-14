package ocraction

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
)

func TestResizeHalfPixelMatchesPinnedPythonFixture(t *testing.T) {
	source := RGBImage{Width: 3, Height: 2, RGB: []byte{
		0, 10, 20, 30, 40, 50, 60, 70, 80,
		90, 100, 110, 120, 130, 140, 150, 160, 170,
	}}
	resized, err := ResizeHalfPixel(source, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 10, 20, 18, 28, 38, 41, 51, 61, 60, 70, 80,
		45, 55, 65, 63, 73, 83, 86, 96, 106, 105, 115, 125,
		90, 100, 110, 108, 118, 128, 131, 141, 151, 150, 160, 170,
	}
	if !reflect.DeepEqual(resized.RGB, want) {
		t.Fatalf("resized RGB = %v, want %v", resized.RGB, want)
	}
	digest := sha256.Sum256(resized.RGB)
	if got := fmt.Sprintf("%x", digest); got != "c1200f53c135ac8613dfa30b513508e8358bdd2ea74864156658855be69d8dd0" {
		t.Fatalf("resize sha256 = %s", got)
	}
}

func TestEvaluateGateUsesStrictPixelThresholdAndInclusiveRatio(t *testing.T) {
	config := GateConfig{
		TopPermille: 0, BottomPermille: 1000,
		OrangeThreshold: 255, CenterLeftPermille: 0, CenterRightPermille: 1000,
		MinimumCenterOrangeRatio: 1, LowOrangeThreshold: 255,
		ActiveColumnPixelRatio: 1, MaximumActiveOrangeColumnRatio: 0,
		HorizontalEdgeThreshold: 60, MinimumHorizontalEdgeRatio: 1,
	}
	atThreshold, err := EvaluateGate(RGBImage{Width: 2, Height: 1, RGB: []byte{0, 0, 0, 60, 60, 60}}, config)
	if err != nil {
		t.Fatal(err)
	}
	if atThreshold.Accepted || atThreshold.HorizontalEdgeRatio != 0 {
		t.Fatalf("edge exactly at threshold must be rejected: %#v", atThreshold)
	}
	aboveThreshold, err := EvaluateGate(RGBImage{Width: 2, Height: 1, RGB: []byte{0, 0, 0, 61, 61, 61}}, config)
	if err != nil {
		t.Fatal(err)
	}
	if !aboveThreshold.Accepted || aboveThreshold.HorizontalEdgeRatio != 1 {
		t.Fatalf("edge ratio at inclusive minimum must be accepted: %#v", aboveThreshold)
	}
}

func TestEvaluateGateAcceptsNarrowCenteredOrangeShape(t *testing.T) {
	image := RGBImage{Width: 4, Height: 2, RGB: make([]byte, 4*2*3)}
	for y := 0; y < image.Height; y++ {
		index := (y*image.Width + 1) * 3
		image.RGB[index], image.RGB[index+1], image.RGB[index+2] = 200, 120, 0
	}
	evidence, err := EvaluateGate(image, GateConfig{
		TopPermille: 0, BottomPermille: 1000,
		OrangeThreshold: 40, CenterLeftPermille: 0, CenterRightPermille: 1000,
		MinimumCenterOrangeRatio: 0.18, LowOrangeThreshold: 10,
		ActiveColumnPixelRatio: 0.05, MaximumActiveOrangeColumnRatio: 0.35,
		HorizontalEdgeThreshold: 255, MinimumHorizontalEdgeRatio: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Accepted || evidence.CenterOrangeRatio != 0.25 || evidence.ActiveOrangeColumnRatio != 0.25 {
		t.Fatalf("gate evidence = %#v", evidence)
	}
}
