// Package assistbackend implements the one-request JSONL protocol used by the
// WinUI AssistGUI to invoke the Go setup backend.
package assistbackend

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	Protocol        = "windows-assist-v1"
	maxRequestBytes = 1 << 20
)

type Command string

const (
	CommandInspect           Command = "inspect"
	CommandInstall           Command = "install"
	CommandUpdate            Command = "update"
	CommandRepair            Command = "repair"
	CommandUninstall         Command = "uninstall"
	CommandStart             Command = "start"
	CommandStop              Command = "stop"
	CommandConfigureWatchdog Command = "configure-watchdog"
	CommandStartTailscale    Command = "start-tailscale"
	CommandStopTailscale     Command = "stop-tailscale"
)

type Request struct {
	Protocol        string    `json:"protocol"`
	RequestID       string    `json:"requestId"`
	ClientProcessID int       `json:"clientProcessId"`
	Command         Command   `json:"command"`
	Settings        *Settings `json:"settings,omitempty"`
}

type Settings struct {
	WatchdogStartAtSignIn *bool  `json:"watchdogStartAtSignIn,omitempty"`
	TailscaleAuthKey      string `json:"tailscaleAuthKey,omitempty"`
}

type Event struct {
	Protocol  string    `json:"protocol"`
	RequestID string    `json:"requestId"`
	Sequence  int       `json:"sequence"`
	Type      string    `json:"type"`
	Phase     string    `json:"phase,omitempty"`
	Message   string    `json:"message,omitempty"`
	Snapshot  *Snapshot `json:"snapshot,omitempty"`
	Success   *bool     `json:"success,omitempty"`
	CloseGUI  bool      `json:"closeGui,omitempty"`
	Code      string    `json:"code,omitempty"`
	Details   string    `json:"details,omitempty"`
}

type Snapshot struct {
	Installed    bool              `json:"installed"`
	Version      *string           `json:"version"`
	Capture      CaptureSnapshot   `json:"capture"`
	Watchdog     WatchdogSnapshot  `json:"watchdog"`
	LANEndpoints []string          `json:"lanEndpoints"`
	Tailscale    TailscaleSnapshot `json:"tailscale"`
}

type CaptureSnapshot struct {
	Running bool `json:"running"`
}

type WatchdogSnapshot struct {
	Installed     bool `json:"installed"`
	Running       bool `json:"running"`
	StartAtSignIn bool `json:"startAtSignIn"`
}

type TailscaleSnapshot struct {
	Status string  `json:"status"`
	IPv4   *string `json:"ipv4"`
	IPv6   *string `json:"ipv6"`
}

var requestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func DecodeRequest(reader io.Reader) (Request, error) {
	limited := io.LimitReader(reader, maxRequestBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	if len(data) > maxRequestBytes {
		return Request{}, fmt.Errorf("request exceeds %d bytes", maxRequestBytes)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return Request{}, errors.New("request must be one newline-terminated JSON value followed by EOF")
	}
	if bytes.Count(data, []byte{'\n'}) != 1 {
		return Request{}, errors.New("request must contain exactly one JSON line")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Request{}, errors.New("request contains multiple JSON values")
		}
		return Request{}, fmt.Errorf("decode request suffix: %w", err)
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	return request, nil
}

func (r Request) Validate() error {
	if r.Protocol != Protocol {
		return fmt.Errorf("protocol must equal %q", Protocol)
	}
	if !requestIDPattern.MatchString(r.RequestID) {
		return errors.New("requestId must be a canonical UUID")
	}
	if r.ClientProcessID <= 0 {
		return errors.New("clientProcessId must be a positive integer")
	}
	switch r.Command {
	case CommandInspect, CommandInstall, CommandUpdate, CommandRepair, CommandUninstall,
		CommandStart, CommandStop, CommandConfigureWatchdog, CommandStartTailscale, CommandStopTailscale:
	default:
		return fmt.Errorf("unsupported command %q", r.Command)
	}
	if r.Settings != nil && r.Settings.TailscaleAuthKey != "" {
		if r.Command != CommandStart && r.Command != CommandStartTailscale {
			return errors.New("tailscaleAuthKey is allowed only for start or start-tailscale")
		}
		for _, value := range []byte(r.Settings.TailscaleAuthKey) {
			if value > 0x7f || value == ' ' || value == '\t' || value == '\r' || value == '\n' {
				return errors.New("tailscaleAuthKey must contain non-whitespace ASCII characters only")
			}
		}
	}
	if r.Settings != nil && r.Settings.WatchdogStartAtSignIn != nil {
		switch r.Command {
		case CommandInstall, CommandUpdate, CommandRepair, CommandConfigureWatchdog:
		default:
			return errors.New("watchdogStartAtSignIn is allowed only for install, update, repair, or configure-watchdog")
		}
	}
	if r.Command == CommandStartTailscale && (r.Settings == nil || r.Settings.TailscaleAuthKey == "") {
		return errors.New("start-tailscale requires tailscaleAuthKey")
	}
	if r.Command == CommandConfigureWatchdog && (r.Settings == nil || r.Settings.WatchdogStartAtSignIn == nil) {
		return errors.New("configure-watchdog requires watchdogStartAtSignIn")
	}
	return nil
}

type Emitter struct {
	encoder  *json.Encoder
	buffer   *bufio.Writer
	request  Request
	sequence int
}

func NewEmitter(writer io.Writer, request Request) *Emitter {
	buffer := bufio.NewWriter(writer)
	return &Emitter{encoder: json.NewEncoder(buffer), buffer: buffer, request: request}
}

func (e *Emitter) Emit(event Event) error {
	e.sequence++
	event.Protocol = Protocol
	event.RequestID = e.request.RequestID
	event.Sequence = e.sequence
	if err := validateEvent(event); err != nil {
		return err
	}
	if err := e.encoder.Encode(event); err != nil {
		return err
	}
	return e.buffer.Flush()
}

func validateEvent(event Event) error {
	switch event.Type {
	case "progress":
		if strings.TrimSpace(event.Phase) == "" || strings.TrimSpace(event.Message) == "" || event.Snapshot != nil || event.Success != nil || event.CloseGUI || event.Code != "" || event.Details != "" {
			return errors.New("progress event requires only phase and message")
		}
	case "snapshot":
		if event.Snapshot == nil || event.Message != "" || event.Success != nil || event.CloseGUI || event.Phase != "" || event.Code != "" || event.Details != "" {
			return errors.New("snapshot event requires only snapshot")
		}
		if err := event.Snapshot.validate(); err != nil {
			return fmt.Errorf("invalid snapshot event: %w", err)
		}
	case "result":
		if event.Success == nil || !*event.Success || strings.TrimSpace(event.Message) == "" || event.Phase != "" || event.Code != "" || event.Details != "" {
			return errors.New("result event requires success=true and message")
		}
	case "error":
		if strings.TrimSpace(event.Code) == "" || strings.TrimSpace(event.Message) == "" || event.Snapshot != nil || event.Success != nil || event.CloseGUI || event.Phase != "" {
			return errors.New("error event requires code and message")
		}
	default:
		return fmt.Errorf("unsupported event type %q", event.Type)
	}
	return nil
}

func (s Snapshot) validate() error {
	if !s.Installed && s.Version != nil {
		return errors.New("not-installed snapshot must use a null version")
	}
	if s.Capture.Running && !s.Installed {
		return errors.New("capture cannot be running when WindowsAgent is not installed")
	}
	if s.Watchdog.Running && !s.Watchdog.Installed {
		return errors.New("Watchdog cannot be running when its task is not installed")
	}
	if s.LANEndpoints == nil {
		return errors.New("lanEndpoints must be an array")
	}
	for _, endpoint := range s.LANEndpoints {
		if strings.TrimSpace(endpoint) == "" {
			return errors.New("lanEndpoints must not contain an empty value")
		}
	}
	switch s.Tailscale.Status {
	case "disabled", "starting", "online", "stopping", "error":
	default:
		return fmt.Errorf("unsupported Tailscale status %q", s.Tailscale.Status)
	}
	return nil
}
