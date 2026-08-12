// Package wgcworker owns the persistent Windows Graphics Capture worker
// process contract and the Agent-side capture adapter.
package wgcworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

const (
	ProtocolVersion           = "2026-08-12-persistent-wgc-borderless-v2"
	MaxFrameBytes             = 128 << 20
	MaxCallDuration           = 60 * time.Second
	MaxInitializationDuration = 2 * time.Minute
)

const (
	methodInitialize    = "initialize"
	methodStatus        = "capture/status"
	methodCapture       = "capture/full"
	methodCaptureRegion = "capture/region"
	methodShutdown      = "shutdown"
)

type initializeParams struct {
	ProtocolVersion string    `json:"protocolVersion"`
	Trace           bool      `json:"trace"`
	Deadline        time.Time `json:"deadline"`
}

type initializeResult struct {
	ProtocolVersion  string         `json:"protocolVersion"`
	ProcessID        int            `json:"processId"`
	Backend          string         `json:"backend"`
	Persistent       bool           `json:"persistent"`
	BorderlessAccess string         `json:"borderlessAccess"`
	BorderRequired   bool           `json:"borderRequired"`
	Status           capture.Status `json:"status"`
}

type deadlineParams struct {
	Deadline time.Time `json:"deadline"`
}

type captureParams struct {
	Deadline time.Time       `json:"deadline"`
	Request  capture.Request `json:"request"`
}

type captureResult struct {
	Result capture.Result `json:"result"`
}

type regionParams struct {
	Deadline time.Time             `json:"deadline"`
	Request  capture.RegionRequest `json:"request"`
}

type regionResult struct {
	Result capture.RegionResult `json:"result"`
}

type shutdownResult struct {
	State string `json:"state"`
}

func validateInitializeResult(result initializeResult, expectedProcessID int) error {
	if result.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("WGC worker protocolVersion must equal %s", ProtocolVersion)
	}
	if result.ProcessID != expectedProcessID || expectedProcessID <= 0 {
		return errors.New("WGC worker processId does not match the launched process")
	}
	if result.Backend != "windows-graphics-capture" || !result.Persistent || !result.Status.Supported {
		return errors.New("WGC worker initialize response does not match the persistent capture contract")
	}
	if result.BorderlessAccess != "allowed" {
		return errors.New("WGC worker borderlessAccess must equal allowed")
	}
	if result.BorderRequired {
		return errors.New("WGC worker borderRequired must equal false")
	}
	return nil
}

type errorData struct {
	Code  string `json:"code"`
	Cause string `json:"cause"`
}

func effectiveDeadline(ctx context.Context, maximum time.Duration) (time.Time, error) {
	if ctx == nil {
		return time.Time{}, errors.New("capture worker context is required")
	}
	if maximum <= 0 || maximum > MaxCallDuration {
		return time.Time{}, fmt.Errorf("capture worker maximum call duration must be from 1 through %s", MaxCallDuration)
	}
	deadline := time.Now().Add(maximum)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if !deadline.After(time.Now()) {
		return time.Time{}, context.DeadlineExceeded
	}
	return deadline.UTC(), nil
}

func effectiveInitializationDeadline(ctx context.Context) (time.Time, error) {
	if ctx == nil {
		return time.Time{}, errors.New("capture worker initialization context is required")
	}
	deadline := time.Now().Add(MaxInitializationDuration)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if !deadline.After(time.Now()) {
		return time.Time{}, context.DeadlineExceeded
	}
	return deadline.UTC(), nil
}

func decodeStrict(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return errors.New("params are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return err
	}
	return nil
}
