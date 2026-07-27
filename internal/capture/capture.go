// Package capture defines the still-capture capability contract.
package capture

import (
	"context"
	"fmt"

	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/rules"
)

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
	PNG                []byte
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
	Capture(context.Context, bool) (Result, error)
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
