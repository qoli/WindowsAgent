// Package capture defines the still-capture capability contract.
package capture

import (
	"context"
	"errors"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

type Profile string

const (
	ProfileNativeJPEG Profile = "native-jpeg"
	Profile1080pJPEG  Profile = "1080p-jpeg"
	ProfileNativePNG  Profile = "native-png"
	DefaultProfile            = ProfileNativeJPEG
)

type Request struct {
	Profile       Profile
	IncludeCursor bool
}

func ParseProfile(value string) (Profile, error) {
	if value == "" {
		return DefaultProfile, nil
	}
	profile := Profile(value)
	switch profile {
	case ProfileNativeJPEG, Profile1080pJPEG, ProfileNativePNG:
		return profile, nil
	default:
		return "", fmt.Errorf("unknown capture profile %q", value)
	}
}

type Monitor struct {
	DeviceName       string  `json:"device_name"`
	Width            int     `json:"width"`
	Height           int     `json:"height"`
	HDR              bool    `json:"hdr"`
	ColorSpace       string  `json:"color_space"`
	MaxLuminanceNits float64 `json:"max_luminance_nits,omitempty"`
}

type Status struct {
	Supported bool    `json:"supported"`
	Monitor   Monitor `json:"primary_monitor"`
}

type Result struct {
	Content            []byte
	Profile            Profile
	Format             string
	ContentType        string
	FileExtension      string
	Quality            int
	ChromaSubsampling  string
	Width              int
	Height             int
	IncludeCursor      bool
	Monitor            Monitor
	Foreground         foreground.Info
	Rule               rules.Resolution
	CapturePixelFormat string
	ToneMapped         bool
}

type Capturer interface {
	Status(context.Context) (Status, error)
	Capture(context.Context, Request) (Result, error)
}

func (r Result) ValidateEncoding() error {
	if len(r.Content) == 0 {
		return errors.New("capture content is empty")
	}
	switch r.Profile {
	case ProfileNativeJPEG, Profile1080pJPEG:
		if r.Format != "jpeg" || r.ContentType != "image/jpeg" || r.FileExtension != ".jpg" {
			return errors.New("JPEG capture encoding metadata is inconsistent")
		}
		if r.Quality != 90 || r.ChromaSubsampling != "444" {
			return errors.New("JPEG capture must use quality 90 and 4:4:4 chroma subsampling")
		}
	case ProfileNativePNG:
		if r.Format != "png" || r.ContentType != "image/png" || r.FileExtension != ".png" {
			return errors.New("PNG capture encoding metadata is inconsistent")
		}
		if r.Quality != 0 || r.ChromaSubsampling != "" {
			return errors.New("PNG capture must not report lossy encoding settings")
		}
	default:
		return fmt.Errorf("invalid capture result profile %q", r.Profile)
	}
	return nil
}

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func Failure(code, message string, err error) error {
	return &Error{Code: code, Message: message, Err: err}
}
