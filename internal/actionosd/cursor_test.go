package actionosd

import "testing"

func TestStartupCursorCombinesBoundedReplayAndExplicitMinimum(t *testing.T) {
	for _, test := range []struct {
		name              string
		last, minimum     uint64
		replayLimit, want int
	}{
		{name: "complete history", last: 20, replayLimit: 100, want: 0},
		{name: "bounded history", last: 120, replayLimit: 100, want: 20},
		{name: "explicit migration boundary", last: 120, minimum: 80, replayLimit: 100, want: 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := StartupCursor(test.last, test.minimum, test.replayLimit)
			if err != nil || got != uint64(test.want) {
				t.Fatalf("cursor = %d, err = %v, want %d", got, err, test.want)
			}
		})
	}
}

func TestStartupCursorRejectsInvalidBoundary(t *testing.T) {
	if _, err := StartupCursor(10, 11, 100); err == nil {
		t.Fatal("future minimum cursor was accepted")
	}
	if _, err := StartupCursor(10, 0, 0); err == nil {
		t.Fatal("non-positive replay limit was accepted")
	}
}
