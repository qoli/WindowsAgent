package ocrregionsaction

import (
	"path/filepath"
	"testing"
)

func TestEliteRequestDockingSearchRegionLoadsWithinRuntimePixelLimit(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(
		"..", "..", "Rules", "EliteDangerous64.exe", "Actions", "request-docking-action-regions",
	))
	if err != nil {
		t.Fatal(err)
	}
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	pixels := uint64(config.ReferenceRegion.Width) * uint64(config.ReferenceRegion.Height)
	if pixels != config.MaxPixels || config.MaxPixels > MaxRegionPixels {
		t.Fatalf("search pixels=%d maxPixels=%d runtimeLimit=%d", pixels, config.MaxPixels, MaxRegionPixels)
	}
}
