package mfvideo

import (
	"image/color"
	"testing"
)

func TestRGB32PositiveStrideIsTopDownAndCropsDisplayAperture(t *testing.T) {
	source := append(rgb32Row(255, 0, 0), rgb32Row(0, 255, 0)...)
	source = append(source, rgb32Row(0, 0, 255)...)
	frame, err := rgb32ToNRGBA(source, 2, 3, 8, 0, 0, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRGB(t, frame.NRGBAAt(0, 0), 255, 0, 0)
	assertRGB(t, frame.NRGBAAt(0, 1), 0, 255, 0)
}

func TestRGB32NegativeStrideIsBottomUp(t *testing.T) {
	source := append(rgb32Row(0, 0, 255), rgb32Row(0, 255, 0)...)
	source = append(source, rgb32Row(255, 0, 0)...)
	frame, err := rgb32ToNRGBA(source, 2, 3, -8, 0, 0, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRGB(t, frame.NRGBAAt(0, 0), 255, 0, 0)
	assertRGB(t, frame.NRGBAAt(0, 1), 0, 255, 0)
}

func rgb32Row(red, green, blue byte) []byte {
	return []byte{blue, green, red, 0, blue, green, red, 0}
}

func assertRGB(t *testing.T, got color.NRGBA, red, green, blue byte) {
	t.Helper()
	if got.R != red || got.G != green || got.B != blue || got.A != 255 {
		t.Fatalf("pixel=%v", got)
	}
}
