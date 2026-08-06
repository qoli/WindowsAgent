package capture

import "testing"

func TestMapReferenceRegionAtFourK(t *testing.T) {
	viewport, physical, err := MapReferenceRegion(3840, 2160, ReferenceRegion{X: 670, Y: 780, Width: 96, Height: 96})
	if err != nil {
		t.Fatal(err)
	}
	if viewport != (PixelRegion{Width: 3840, Height: 2160}) {
		t.Fatalf("viewport = %#v", viewport)
	}
	if physical != (PixelRegion{Left: 1340, Top: 1560, Width: 192, Height: 192}) {
		t.Fatalf("physical = %#v", physical)
	}
}

func TestMapReferenceRegionCentersSixteenByNineOnUltrawide(t *testing.T) {
	viewport, physical, err := MapReferenceRegion(3440, 1440, ReferenceRegion{X: 0, Y: 0, Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	if viewport != (PixelRegion{Left: 440, Width: 2560, Height: 1440}) {
		t.Fatalf("viewport = %#v", viewport)
	}
	if physical != viewport {
		t.Fatalf("physical = %#v, want viewport %#v", physical, viewport)
	}
}

func TestMapReferenceRegionCentersSixteenByNineOnTallFrame(t *testing.T) {
	viewport, physical, err := MapReferenceRegion(1920, 1200, ReferenceRegion{X: 0, Y: 0, Width: 1920, Height: 1080})
	if err != nil {
		t.Fatal(err)
	}
	if viewport != (PixelRegion{Top: 60, Width: 1920, Height: 1080}) {
		t.Fatalf("viewport = %#v", viewport)
	}
	if physical != viewport {
		t.Fatalf("physical = %#v, want viewport %#v", physical, viewport)
	}
}

func TestMapReferenceRegionRoundsOutward(t *testing.T) {
	_, physical, err := MapReferenceRegion(2560, 1440, ReferenceRegion{X: 1, Y: 1, Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if physical != (PixelRegion{Left: 1, Top: 1, Width: 2, Height: 2}) {
		t.Fatalf("physical = %#v", physical)
	}
}

func TestMapReferenceRegionRejectsOutsideReferenceSpace(t *testing.T) {
	if _, _, err := MapReferenceRegion(3840, 2160, ReferenceRegion{X: 1900, Y: 1000, Width: 21, Height: 80}); err == nil {
		t.Fatal("out-of-bounds reference region was accepted")
	}
}
