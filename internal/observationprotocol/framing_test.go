package observationprotocol

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestConnRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	writer, err := NewConn(strings.NewReader(""), &wire, 1024)
	if err != nil {
		t.Fatal(err)
	}
	want := Message{
		JSONRPC: "2.0",
		ID:      "request-1",
		Method:  "observer/call",
		Params:  []byte(`{"value":1}`),
	}
	if err := writer.Write(want); err != nil {
		t.Fatal(err)
	}
	reader, err := NewConn(&wire, &bytes.Buffer{}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Method != want.Method || string(got.Params) != string(want.Params) {
		t.Fatalf("unexpected message: %#v", got)
	}
}

func TestConnRejectsLFOnlyHeaders(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"1","method":"test"}`
	wire := "Content-Length: 42\nContent-Type: application/windowsagent-observation+json; charset=utf-8\n\n" + body
	conn, err := NewConn(strings.NewReader(wire), &bytes.Buffer{}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err == nil || !strings.Contains(err.Error(), "CRLF") {
		t.Fatalf("expected CRLF error, got %v", err)
	}
}

func TestConnRejectsOversizedFrameBeforeReadingBody(t *testing.T) {
	wire := "Content-Length: 4096\r\nContent-Type: application/windowsagent-observation+json; charset=utf-8\r\n\r\n"
	conn, err := NewConn(strings.NewReader(wire), &bytes.Buffer{}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestConnRejectsTrailingJSONValue(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"1","method":"initialize"}{}`
	frame := fmt.Sprintf(
		"Content-Length: %d\r\nContent-Type: %s\r\n\r\n%s",
		len(body),
		contentType,
		body,
	)
	conn, err := NewConn(strings.NewReader(frame), io.Discard, DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err == nil {
		t.Fatal("Read accepted multiple JSON values")
	}
}

func TestConnRejectsDuplicateJSONKey(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"1","id":"2","method":"initialize"}`
	frame := fmt.Sprintf(
		"Content-Length: %d\r\nContent-Type: %s\r\n\r\n%s",
		len(body),
		contentType,
		body,
	)
	conn, err := NewConn(strings.NewReader(frame), io.Discard, DefaultMaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(); err == nil {
		t.Fatal("Read accepted duplicate JSON key")
	}
}
