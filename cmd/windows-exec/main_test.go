package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/windowsexec"
)

func TestRunBuildsStructuredRequestAndWaitsForCompletion(t *testing.T) {
	stdinPath := filepath.Join(t.TempDir(), "stdin.bin")
	stdin := []byte{0, 1, 2, 255}
	if err := os.WriteFile(stdinPath, stdin, 0o600); err != nil {
		t.Fatal(err)
	}
	var received windowsexec.Request
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
			w.Header().Set("Location", "/v1/action-invocations/exec_1")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_1","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/exec_1/stop"}}`)
		case "/v1/action-invocations/exec_1":
			if r.Method != http.MethodGet {
				t.Errorf("status method = %s", r.Method)
			}
			if statusReads.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"invocationId":"exec_1","state":"RUNNING"}`)
				return
			}
			_, _ = io.WriteString(w, `{"invocationId":"exec_1","state":"COMPLETED","output":{"exitCode":0}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previousPollInterval := pollInterval
	pollInterval = time.Millisecond
	defer func() { pollInterval = previousPollInterval }()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"run", "--url", server.URL, "--executable", `C:\Tools\worker.exe`,
		"--arg", "--mode", "--arg", "repair", "--cwd", `C:\Work`,
		"--env", "MODE=repair", "--env", "EMPTY=", "--window", "hidden",
		"--stdin-file", stdinPath, "--max-output-bytes", "8192", "--timeout", "1500ms",
	}, &stdout, &stderr, server.Client())
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if received.SchemaVersion != windowsexec.SchemaVersion || received.Operation != windowsexec.OperationRun || received.Executable != `C:\Tools\worker.exe` || received.ScriptPath != "" ||
		len(received.Argv) != 2 || received.Argv[0] != "--mode" || received.Argv[1] != "repair" || received.Cwd != `C:\Work` ||
		received.Env["MODE"] != "repair" || received.Env["EMPTY"] != "" || received.Window != windowsexec.WindowHidden ||
		received.MaxOutputBytes != 8192 || received.TimeoutMilliseconds != 1500 || !bytes.Equal(received.Stdin, stdin) {
		t.Fatalf("request = %+v, stdin base64 = %s", received, base64.StdEncoding.EncodeToString(received.Stdin))
	}
	if statusReads.Load() != 2 || !strings.Contains(stdout.String(), `"state":"COMPLETED"`) || !strings.HasSuffix(stdout.String(), "\n") {
		t.Fatalf("status reads = %d, stdout = %q", statusReads.Load(), stdout.String())
	}
}

func TestPS1UsesRemoteScriptPath(t *testing.T) {
	var received windowsexec.Request
	server := terminalServer(t, &received, "COMPLETED")
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"ps1", "--url", server.URL, "--script-path", `D:\AgentStaging\repair.ps1`,
		"--arg", "-Mode", "--arg", "Repair", "--window", "normal",
	}, &stdout, &stderr, server.Client())
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if received.Operation != windowsexec.OperationPowerShellFile || received.ScriptPath != `D:\AgentStaging\repair.ps1` || received.Executable != "" || received.Window != windowsexec.WindowNormal {
		t.Fatalf("request = %+v", received)
	}
}

func TestStartRejectsWaitOnlyOptionsBeforeHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP request must not be made")
		return nil, nil
	})}
	for _, option := range []string{"--stdin-file", "--max-output-bytes", "--timeout"} {
		args := []string{"start", "--url", "http://agent.test:8787", "--executable", `C:\app.exe`}
		switch option {
		case "--stdin-file":
			path := filepath.Join(t.TempDir(), "stdin")
			if err := os.WriteFile(path, []byte("input"), 0o600); err != nil {
				t.Fatal(err)
			}
			args = append(args, option, path)
		case "--max-output-bytes":
			args = append(args, option, "1")
		case "--timeout":
			args = append(args, option, "1s")
		}
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, client); code != 2 || !strings.Contains(stderr.String(), "start does not accept") {
			t.Fatalf("option %s: code = %d, stderr = %q", option, code, stderr.String())
		}
	}
}

func TestRejectsRawCommandAndAmbiguousEnvironmentBeforeHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP request must not be made")
		return nil, nil
	})}
	tests := [][]string{
		{"run", "--url", "http://agent.test:8787", "--executable", "tool.exe", "tool.exe /c raw"},
		{"run", "--url", "http://agent.test:8787", "--executable", "tool.exe", "--env", "MISSING"},
		{"run", "--url", "http://agent.test:8787", "--executable", "tool.exe", "--env", "A=1", "--env", "A=2"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, client); code != 2 || stderr.Len() == 0 {
			t.Fatalf("args = %v, code = %d, stderr = %q", args, code, stderr.String())
		}
	}
}

func TestTerminalFailureIsPrintedAndReturnsFailure(t *testing.T) {
	server := terminalServer(t, nil, "FAILED")
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
	if code != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"state":"FAILED"`) {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestInterruptRequestsStopOnceAndWaitsForCancelledTerminal(t *testing.T) {
	var stopRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case invocationPath:
			w.Header().Set("Location", "/v1/action-invocations/exec_interrupt")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_interrupt","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/exec_interrupt/stop"}}`)
		case "/v1/action-invocations/exec_interrupt/stop":
			stopRequests.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_interrupt","state":"CANCELLING"}`)
		case "/v1/action-invocations/exec_interrupt":
			state := "RUNNING"
			if stopRequests.Load() != 0 {
				state = "CANCELLED"
			}
			_, _ = io.WriteString(w, `{"invocationId":"exec_interrupt","state":"`+state+`"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previousNotify, previousStop := notifyInterrupt, stopInterrupt
	notifyInterrupt = func(channel chan<- os.Signal, _ ...os.Signal) { channel <- os.Interrupt }
	stopInterrupt = func(chan<- os.Signal) {}
	defer func() { notifyInterrupt, stopInterrupt = previousNotify, previousStop }()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
	if code != 1 || stopRequests.Load() != 1 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"state":"CANCELLED"`) {
		t.Fatalf("code = %d, stop requests = %d, stdout = %q, stderr = %q", code, stopRequests.Load(), stdout.String(), stderr.String())
	}
}

func TestCompletedStatusWinsOverPendingInterrupt(t *testing.T) {
	var stopRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case invocationPath:
			w.Header().Set("Location", "/v1/action-invocations/exec_done")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_done","state":"RUNNING"}`)
		case "/v1/action-invocations/exec_done":
			_, _ = io.WriteString(w, `{"invocationId":"exec_done","state":"COMPLETED"}`)
		case "/v1/action-invocations/exec_done/stop":
			stopRequests.Add(1)
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previousNotify, previousStop := notifyInterrupt, stopInterrupt
	notifyInterrupt = func(channel chan<- os.Signal, _ ...os.Signal) { channel <- os.Interrupt }
	stopInterrupt = func(chan<- os.Signal) {}
	defer func() { notifyInterrupt, stopInterrupt = previousNotify, previousStop }()
	var stdout, stderr bytes.Buffer
	code := run([]string{"start", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
	if code != 0 || stopRequests.Load() != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"state":"COMPLETED"`) {
		t.Fatalf("code = %d, stop requests = %d, stdout = %q, stderr = %q", code, stopRequests.Load(), stdout.String(), stderr.String())
	}
}

func TestInterruptStopFailureIsTerminalForClient(t *testing.T) {
	var statusRequests atomic.Int32
	statusStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case invocationPath:
			w.Header().Set("Location", "/v1/action-invocations/exec_stop_failure")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_stop_failure","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/exec_stop_failure/stop"}}`)
		case "/v1/action-invocations/exec_stop_failure":
			statusRequests.Add(1)
			select {
			case <-statusStarted:
			default:
				close(statusStarted)
			}
			_, _ = io.WriteString(w, `{"invocationId":"exec_stop_failure","state":"RUNNING"}`)
		case "/v1/action-invocations/exec_stop_failure/stop":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"stop_rejected"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previousNotify, previousStop := notifyInterrupt, stopInterrupt
	notifyInterrupt = func(channel chan<- os.Signal, _ ...os.Signal) {
		go func() {
			<-statusStarted
			channel <- os.Interrupt
		}()
	}
	stopInterrupt = func(chan<- os.Signal) {}
	defer func() { notifyInterrupt, stopInterrupt = previousNotify, previousStop }()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
	if code != 1 || statusRequests.Load() != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "HTTP 409") {
		t.Fatalf("code = %d, status requests = %d, stdout = %q, stderr = %q", code, statusRequests.Load(), stdout.String(), stderr.String())
	}
}

func TestRemotePathValidationFailsBeforeHTTP(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP request must not be made")
		return nil, nil
	})}
	for _, args := range [][]string{
		{"ps1", "--url", "http://agent.test:8787", "--script-path", "relative.ps1"},
		{"run", "--url", "http://agent.test:8787", "--executable", "tool.exe", "--cwd", "relative"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, client); code != 2 || !strings.Contains(stderr.String(), "absolute Windows path") {
			t.Fatalf("args = %v, code = %d, stderr = %q", args, code, stderr.String())
		}
	}
}

func TestRequiresAcceptedLocationAndStrictInvocationJSON(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header bool
		body   string
		want   string
	}{
		{name: "wrong status", status: http.StatusOK, header: true, body: `{"invocationId":"x","state":"RUNNING"}`, want: "HTTP 200"},
		{name: "missing location", status: http.StatusAccepted, body: `{"invocationId":"x","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/x/stop"}}`, want: "missing required Location"},
		{name: "duplicate JSON key", status: http.StatusAccepted, header: true, body: `{"invocationId":"x","state":"RUNNING","state":"FAILED"}`, want: "duplicate JSON object key"},
		{name: "unknown JSON field", status: http.StatusAccepted, header: true, body: `{"invocationId":"x","state":"RUNNING","surprise":true}`, want: "unknown field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.header {
					w.Header().Set("Location", "/v1/action-invocations/x")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := run([]string{"run", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
			if code != 1 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
		})
	}
}

func TestDoesNotFollowRedirect(t *testing.T) {
	redirected := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == invocationPath {
			http.Redirect(w, r, "/unexpected", http.StatusTemporaryRedirect)
			return
		}
		redirected = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--url", server.URL, "--executable", "tool.exe"}, &stdout, &stderr, server.Client())
	if code != 1 || redirected || !strings.Contains(stderr.String(), "HTTP 307") {
		t.Fatalf("code = %d, redirected = %t, stderr = %q", code, redirected, stderr.String())
	}
}

func terminalServer(t *testing.T, received *windowsexec.Request, state string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case invocationPath:
			if received != nil {
				if err := json.NewDecoder(r.Body).Decode(received); err != nil {
					t.Error(err)
				}
			}
			w.Header().Set("Location", "/v1/action-invocations/exec_terminal")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"invocationId":"exec_terminal","state":"RUNNING","stop":{"method":"POST","url":"/v1/action-invocations/exec_terminal/stop"}}`)
		case "/v1/action-invocations/exec_terminal":
			_, _ = io.WriteString(w, `{"invocationId":"exec_terminal","state":"`+state+`"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
