package pixels

import (
	"image"
	"image/color"
	"testing"
)

func TestNRGBAToBGRNative(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 40, G: 50, B: 60, A: 255})
	got, err := NRGBAToBGR(source, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{30, 20, 10, 60, 50, 40}
	if string(got) != string(want) {
		t.Fatalf("BGR = %v, want %v", got, want)
	}
}

func TestNRGBAToBGRDownscaleInterpolates(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 100, G: 0, B: 0, A: 255})
	source.SetNRGBA(0, 1, color.NRGBA{R: 0, G: 100, B: 0, A: 255})
	source.SetNRGBA(1, 1, color.NRGBA{R: 0, G: 0, B: 100, A: 255})
	got, err := NRGBAToBGR(source, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{25, 25, 25}
	if string(got) != string(want) {
		t.Fatalf("BGR = %v, want %v", got, want)
	}
}
