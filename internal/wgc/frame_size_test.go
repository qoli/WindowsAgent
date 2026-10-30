package wgc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/qoli/WindowsAgent/internal/capture"
)

type sizedTestFrame struct {
	id      int
	content frameSize
}

func TestSizedFrameDiscardsClippedAndPaddedFramesAcrossDisplayModes(t *testing.T) {
	qhd, uhd := frameSize{2560, 1440}, frameSize{3840, 2160}
	size := qhd
	frames := []sizedTestFrame{{1, uhd}, {2, uhd}, {3, qhd}, {4, qhd}, {5, qhd}}
	var closed, retained []int
	var resized []frameSize
	next := func(context.Context) (sizedTestFrame, frameSize, error) {
		if len(frames) == 0 {
			t.Fatal("acquired more frames than the mode changes require")
		}
		frame := frames[0]
		frames = frames[1:]
		return frame, frame.content, nil
	}
	discard := func(frame sizedTestFrame) { closed = append(closed, frame.id) }
	resize := func(next frameSize) error {
		// WGC requires the checked-out frame to be returned before Recreate.
		wantReleased := []int{1, 3}[len(resized)]
		if len(closed) == 0 || closed[len(closed)-1] != wantReleased {
			t.Fatal("resized while the old-size surface was still checked out")
		}
		resized = append(resized, next)
		return nil
	}
	for range 3 {
		frame, err := acquireSizedFrame(context.Background(), &size, next, discard, resize)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, frame.id)
		if frame.content != size {
			t.Fatalf("returned frame %d with stale pool dimensions", frame.id)
		}
	}
	if !reflect.DeepEqual(retained, []int{2, 4, 5}) || !reflect.DeepEqual(closed, []int{1, 3}) {
		t.Fatalf("retained=%v closed=%v; clipped/padded frames must not escape", retained, closed)
	}
	if !reflect.DeepEqual(resized, []frameSize{uhd, qhd}) || size != qhd {
		t.Fatalf("resized=%v final=%v", resized, size)
	}
}

func TestSizedFrameFollowsAnotherResizeBeforeTheNextFrame(t *testing.T) {
	size := frameSize{2560, 1440}
	content := []frameSize{{3840, 2160}, {1920, 1080}, {1920, 1080}}
	var released, recreations int
	frame, err := acquireSizedFrame(context.Background(), &size,
		func(context.Context) (int, frameSize, error) {
			next := content[0]
			content = content[1:]
			return 3 - len(content), next, nil
		}, func(int) { released++ }, func(frameSize) error { recreations++; return nil })
	if err != nil || frame != 3 || released != 2 || recreations != 2 || size != (frameSize{1920, 1080}) {
		t.Fatalf("frame=%d size=%v released=%d recreations=%d err=%v", frame, size, released, recreations, err)
	}
}

func TestSizedFramePreservesAcquisitionAndResizeErrors(t *testing.T) {
	for _, stage := range []string{"acquire", "resize"} {
		t.Run(stage, func(t *testing.T) {
			original := capture.Failure("capture_session_failed", "native operation failed", errors.New("HRESULT"))
			initial := frameSize{2560, 1440}
			size := initial
			var released, resized int
			frame, err := acquireSizedFrame(context.Background(), &size,
				func(context.Context) (int, frameSize, error) {
					if stage == "acquire" {
						return 0, frameSize{}, original
					}
					return 1, frameSize{3840, 2160}, nil
				}, func(int) { released++ }, func(frameSize) error { resized++; return original })
			if frame != 0 || err != original || size != initial {
				t.Fatalf("frame=%d size=%v err=%v", frame, size, err)
			}
			want := 0
			if stage == "resize" {
				want = 1
			}
			if released != want || resized != want {
				t.Fatalf("released=%d resized=%d, want %d", released, resized, want)
			}
		})
	}
}

func TestSizedFrameRejectsInvalidContentBeforeReadback(t *testing.T) {
	for _, content := range []frameSize{{0, 2160}, {3840, 0}, {-1, 1440}} {
		size := frameSize{2560, 1440}
		released := false
		_, err := acquireSizedFrame(context.Background(), &size,
			func(context.Context) (int, frameSize, error) { return 1, content, nil },
			func(int) { released = true }, func(frameSize) error { t.Fatal("recreated with invalid dimensions"); return nil })
		var failure *capture.Error
		if !released || !errors.As(err, &failure) || failure.Code != "capture_frame_failed" {
			t.Fatalf("content=%v released=%t err=%v", content, released, err)
		}
	}
}

func TestSizedFrameKeepsOriginalContextAcrossResize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	size := frameSize{2560, 1440}
	calls, releases := 0, 0
	_, err := acquireSizedFrame(ctx, &size,
		func(actual context.Context) (int, frameSize, error) {
			calls++
			if actual != ctx {
				t.Fatal("resize substituted a new request context")
			}
			return 1, frameSize{3840, 2160}, nil
		}, func(int) { releases++ }, func(frameSize) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 || releases != 1 {
		t.Fatalf("calls=%d releases=%d err=%v", calls, releases, err)
	}
}
