package assistbackend

import (
	"bytes"
	"strings"
	"testing"
)

const testRequestID = "123e4567-e89b-42d3-a456-426614174000"

func TestDecodeRequestRequiresOneStrictJSONLineAndEOF(t *testing.T) {
	request, err := DecodeRequest(strings.NewReader(`{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","clientProcessId":42,"command":"inspect"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != CommandInspect || request.ClientProcessID != 42 {
		t.Fatalf("request = %+v", request)
	}
	for name, input := range map[string]string{
		"missing newline": `{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","clientProcessId":42,"command":"inspect"}`,
		"second line":     "{}\n{}\n",
		"unknown field":   `{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","clientProcessId":42,"command":"inspect","extra":true}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRequest(strings.NewReader(input)); err == nil {
				t.Fatal("invalid request unexpectedly accepted")
			}
		})
	}
}

func TestRequestDoesNotAllowAuthKeyOnUnrelatedCommand(t *testing.T) {
	request := Request{Protocol: Protocol, RequestID: testRequestID, ClientProcessID: 42, Command: CommandStop,
		Settings: &Settings{TailscaleAuthKey: "tskey-secret"}}
	if err := request.Validate(); err == nil {
		t.Fatal("auth key on stop unexpectedly accepted")
	}
}

func TestRequestDoesNotAllowWatchdogSettingOnUnrelatedCommand(t *testing.T) {
	setting := true
	request := Request{Protocol: Protocol, RequestID: testRequestID, ClientProcessID: 42, Command: CommandStop,
		Settings: &Settings{WatchdogStartAtSignIn: &setting}}
	if err := request.Validate(); err == nil {
		t.Fatal("Watchdog setting on stop unexpectedly accepted")
	}
}

func TestEmitterRejectsResultWithSuccessFalse(t *testing.T) {
	request := Request{Protocol: Protocol, RequestID: testRequestID, ClientProcessID: 42, Command: CommandInspect}
	var output bytes.Buffer
	emitter := NewEmitter(&output, request)
	failed := false
	if err := emitter.Emit(Event{Type: "result", Success: &failed, Message: "not successful"}); err == nil {
		t.Fatal("result with success=false unexpectedly accepted")
	}
}

func TestEmitterGoldenWinUIJSONShapes(t *testing.T) {
	request := Request{Protocol: Protocol, RequestID: testRequestID, ClientProcessID: 42, Command: CommandInspect}
	var output bytes.Buffer
	emitter := NewEmitter(&output, request)
	ipv4 := "100.64.0.1"
	version := "0.1.5"
	snapshot := Snapshot{
		Installed:    true,
		Version:      &version,
		Capture:      CaptureSnapshot{Running: true},
		Watchdog:     WatchdogSnapshot{Installed: true, Running: true, StartAtSignIn: false},
		LANEndpoints: []string{"http://192.168.1.2:8787"},
		Tailscale:    TailscaleSnapshot{Status: "online", IPv4: &ipv4, IPv6: nil},
	}
	for _, event := range []Event{
		{Type: "progress", Phase: "inspect", Message: "Reading state"},
		{Type: "snapshot", Snapshot: &snapshot},
		successEvent("Inspection completed", false),
		{Type: "error", Code: "INSPECTION_FAILED", Message: "failed", Details: "detail"},
	} {
		if err := emitter.Emit(event); err != nil {
			t.Fatal(err)
		}
	}
	want := "" +
		`{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","sequence":1,"type":"progress","phase":"inspect","message":"Reading state"}` + "\n" +
		`{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","sequence":2,"type":"snapshot","snapshot":{"installed":true,"version":"0.1.5","capture":{"running":true},"watchdog":{"installed":true,"running":true,"startAtSignIn":false},"lanEndpoints":["http://192.168.1.2:8787"],"tailscale":{"status":"online","ipv4":"100.64.0.1","ipv6":null}}}` + "\n" +
		`{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","sequence":3,"type":"result","message":"Inspection completed","success":true}` + "\n" +
		`{"protocol":"windows-assist-v1","requestId":"123e4567-e89b-42d3-a456-426614174000","sequence":4,"type":"error","message":"failed","code":"INSPECTION_FAILED","details":"detail"}` + "\n"
	if output.String() != want {
		t.Fatalf("JSONL mismatch\n got: %s\nwant: %s", output.String(), want)
	}
}

func TestCleanSnapshotKeepsConcreteWatchdogShapeAndNullIPs(t *testing.T) {
	request := Request{Protocol: Protocol, RequestID: testRequestID, ClientProcessID: 42, Command: CommandInspect}
	var output bytes.Buffer
	emitter := NewEmitter(&output, request)
	if err := emitter.Emit(Event{Type: "snapshot", Snapshot: &Snapshot{
		Capture: CaptureSnapshot{}, Watchdog: WatchdogSnapshot{}, LANEndpoints: []string{},
		Tailscale: TailscaleSnapshot{Status: "disabled"},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"version":null`,
		`"watchdog":{"installed":false,"running":false,"startAtSignIn":false}`,
		`"lanEndpoints":[]`,
		`"tailscale":{"status":"disabled","ipv4":null,"ipv6":null}`,
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("snapshot missing %s: %s", fragment, output.String())
		}
	}
}
