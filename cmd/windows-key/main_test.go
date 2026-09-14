package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/inputaction"
)

func TestRunPressBuildsPinnedRequestAndWaitsForCompletion(t *testing.T) {
	var received inputaction.DirectPressRequest
	var statusReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case invocationPath:
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("invoke request = %s content-type %q", r.Method, r.Header.Get("Content-Type"))
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&received); err != nil {
				t.Error(err)
			}
			w.Header().Set("Location", "/v1/action-invocations/key_1")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"key_1","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/key_1/stop"}}`)
		case "/v1/action-invocations/key_1":
			if statusReads.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"invocationId":"key_1","state":"RUNNING"}`)
				return
			}
			_, _ = io.WriteString(w, `{"invocationId":"key_1","state":"COMPLETED","output":{"operation":"press"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previous := pollInterval
	pollInterval = time.Millisecond
	defer func() { pollInterval = previous }()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"press", "--url", server.URL, "--key", "Key_Home", "--hold", "180ms",
		"--expected-process-id", "42", "--expected-executable-name", "Game.exe",
		"--expected-executable-path", `C:\Games\Game.exe`,
	}, &stdout, &stderr, server.Client())
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if received.SchemaVersion != inputaction.DirectSchemaVersion || received.Key != "Key_Home" || received.HoldMS != 180 ||
		received.ExpectedForeground.ProcessID != 42 || received.ExpectedForeground.ExecutablePath != `C:\Games\Game.exe` {
		t.Fatalf("request=%+v", received)
	}
	if statusReads.Load() != 2 || !strings.Contains(stdout.String(), `"state":"COMPLETED"`) {
		t.Fatalf("reads=%d stdout=%q", statusReads.Load(), stdout.String())
	}
}

func TestRunRejectsUnpinnedOrNoncanonicalInputBeforeHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP request must not be made")
		return nil, nil
	})}
	for _, args := range [][]string{
		{"press", "--url", "http://agent.test:8787", "--key", "Key_Home", "--hold", "40ms"},
		{"press", "--url", "http://agent.test:8787", "--key", "Home", "--hold", "40ms", "--expected-process-id", "42", "--expected-executable-name", "Game.exe", "--expected-executable-path", `C:\Games\Game.exe`},
		{"press", "--url", "http://agent.test:8787", "--key", "Key_Home", "--hold", "40.5ms", "--expected-process-id", "42", "--expected-executable-name", "Game.exe", "--expected-executable-path", `C:\Games\Game.exe`},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, client); code != 2 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
