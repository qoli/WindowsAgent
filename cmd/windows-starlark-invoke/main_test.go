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
	"testing"

	"github.com/qoli/WindowsAgent/internal/windowsautomation"
)

func TestRunPreflightsArchivesAndPrintsRemoteResponse(t *testing.T) {
	root := writeInvokePackage(t)
	inputsPath := filepath.Join(t.TempDir(), "inputs.json")
	writeInvokeFile(t, inputsPath, `{"value":"remote"}`)
	var request invocationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != invocationPath || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s, content-type = %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(data, &request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"state":"RUNNING","runtimeError":true}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--url", server.URL, "--package", root, "--inputs", inputsPath}, &stdout, &stderr, server.Client())
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != "{\"state\":\"RUNNING\",\"runtimeError\":true}" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if request.SchemaVersion != 1 || request.Inputs["value"] != "remote" {
		t.Fatalf("request = %+v", request)
	}
	archive, err := base64.StdEncoding.DecodeString(request.PackageBase64)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := windowsautomation.LoadArchive(archive)
	if err != nil || pkg.Manifest.Title != "Hello Windows" {
		t.Fatalf("package = %+v, error = %v", pkg, err)
	}
	wantArchive, err := windowsautomation.ArchiveDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, wantArchive) {
		t.Fatal("uploaded archive is not the canonical deterministic archive")
	}
}

func TestRunRejectsInvalidInputsBeforeHTTP(t *testing.T) {
	root := writeInvokePackage(t)
	inputsPath := filepath.Join(t.TempDir(), "inputs.json")
	writeInvokeFile(t, inputsPath, `{"value":"one","value":"two"}`)
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP request must not be made")
		return nil, nil
	})}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--url", "http://agent.test:8787", "--package", root, "--inputs", inputsPath}, &stdout, &stderr, client)
	if code != 1 || !strings.Contains(stderr.String(), "duplicate JSON object key") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunFailsOnNon2xxWithoutPrintingResponse(t *testing.T) {
	root := writeInvokePackage(t)
	inputsPath := filepath.Join(t.TempDir(), "inputs.json")
	writeInvokeFile(t, inputsPath, `{"value":"remote"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_starlark_action"}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--url", server.URL, "--package", root, "--inputs", inputsPath}, &stdout, &stderr, server.Client())
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "HTTP 400") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunDoesNotFollowRedirect(t *testing.T) {
	root := writeInvokePackage(t)
	inputsPath := filepath.Join(t.TempDir(), "inputs.json")
	writeInvokeFile(t, inputsPath, `{"value":"remote"}`)
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
	code := run([]string{"--url", server.URL, "--package", root, "--inputs", inputsPath}, &stdout, &stderr, server.Client())
	if code != 1 || redirected || !strings.Contains(stderr.String(), "HTTP 307") {
		t.Fatalf("code = %d, redirected = %t, stderr = %q", code, redirected, stderr.String())
	}
}

func TestRunRequiresExplicitFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, http.DefaultClient); code != 2 || !strings.Contains(stderr.String(), "--url, --package, and --inputs") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func writeInvokePackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"manifest.json":      `{"schemaVersion":1,"version":1,"title":"Hello Windows","entrypoint":"main.star","taskDocument":"TASK.md","inputSchema":"input.schema.json","outputSchema":"output.schema.json","files":["main.star","TASK.md","input.schema.json","output.schema.json"],"limits":{"wallTimeMs":5000,"maxSteps":100000,"maxResultBytes":4096,"maxProcessOutputBytes":4096}}`,
		"main.star":          `def main(ctx): return {"value": ctx.inputs["value"]}`,
		"TASK.md":            "# Hello Windows\n",
		"input.schema.json":  `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`,
		"output.schema.json": `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`,
	}
	for name, content := range files {
		writeInvokeFile(t, filepath.Join(root, name), content)
	}
	return root
}

func writeInvokeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
