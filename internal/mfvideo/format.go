// Package mfvideo writes strict H.264 MP4 video segments through Windows
// Media Foundation.
package mfvideo

import "errors"

type Format struct {
	Width           int
	Height          int
	FramesPerSecond int
	Bitrate         uint32
}

func (f Format) Validate() error {
	if f.Width != 1920 || f.Height != 1080 || f.FramesPerSecond != 1 {
		return errors.New("Media Foundation evidence format must be 1920x1080 at 1 FPS")
	}
	if f.Bitrate < 1_000_000 || f.Bitrate > 20_000_000 {
		return errors.New("Media Foundation evidence bitrate must be between 1000000 and 20000000")
	}
	return nil
}

func (f Format) FrameBytes() int { return f.Width * f.Height * 4 }
