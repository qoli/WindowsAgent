package wgc

import (
	"context"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/capture"
)

type frameSize struct {
	width, height int32
}

// acquireSizedFrame never returns a frame backed by buffers from a different
// content size. WGC clips larger content and leaves undefined pixels around
// smaller content until the pool is recreated. Release that frame before
// resizing, then acquire a new one within the original request deadline.
func acquireSizedFrame[T any](ctx context.Context, size *frameSize,
	next func(context.Context) (T, frameSize, error), discard func(T), resize func(frameSize) error,
) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		frame, content, err := next(ctx)
		if err != nil {
			return zero, err
		}
		if content.width <= 0 || content.height <= 0 {
			discard(frame)
			return zero, capture.Failure("capture_frame_failed",
				fmt.Sprintf("WGC frame content size is invalid: %dx%d", content.width, content.height), nil)
		}
		if content == *size {
			return frame, nil
		}
		discard(frame)
		if err := resize(content); err != nil {
			return zero, err
		}
		*size = content
	}
}
