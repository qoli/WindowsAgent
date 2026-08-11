package videocapture

import (
	"testing"
	"time"
)

func TestFrameValidationRejectsNonVideoEvidence(t *testing.T) {
	valid := Frame{
		Sequence: 1, ScheduledAt: time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
		ObservedAt: time.Date(2026, 8, 11, 1, 2, 3, 10, time.UTC), ForegroundExecutable: "Game.exe",
		Width: 1920, Height: 1080, PixelFormat: PixelFormatBGRX32BottomUp,
		Pixels: make([]byte, 1920*1080*4),
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.PixelFormat = "image/jpeg"
	if err := invalid.Validate(); err == nil {
		t.Fatal("JPEG frame unexpectedly satisfied the video stream contract")
	}
}

func TestSampleRequiresExactlyFrameOrGap(t *testing.T) {
	scheduled := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)
	if err := (Sample{ScheduledAt: scheduled, Stage: "wgc_frame"}).Validate(); err == nil {
		t.Fatal("gap without cause unexpectedly validated")
	}
}
