package mfvideo

import "testing"

func TestFormatIsStrictOneFPS1080P(t *testing.T) {
	valid := Format{Width: 1920, Height: 1080, FramesPerSecond: 1, Bitrate: 4_000_000}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.FramesPerSecond = 2
	if err := invalid.Validate(); err == nil {
		t.Fatal("non-1-FPS format unexpectedly validated")
	}
}
