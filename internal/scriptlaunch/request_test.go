package scriptlaunch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRequest(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(name, []byte(`{
	  "inputs": {"value": 7}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := ReadRequest(name)
	if err != nil {
		t.Fatal(err)
	}
	if request.Inputs["value"] != float64(7) {
		t.Fatalf("request = %#v", request)
	}
}

func TestReadRequestAcceptsBoundedTextRegionPayload(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	payload := `{"inputs":{"pixels":"` + strings.Repeat("a", 128<<10) + `"}}`
	if err := os.WriteFile(name, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(name); err != nil {
		t.Fatalf("bounded text-region payload was rejected: %v", err)
	}
}

func TestReadRequestRejectsPayloadOverBound(t *testing.T) {
	name := filepath.Join(t.TempDir(), "request.json")
	payload := `{"inputs":{"pixels":"` + strings.Repeat("a", MaxRequestBytes) + `"}}`
	if err := os.WriteFile(name, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(name); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized request error = %v", err)
	}
}

func TestReadRequestRejectsInvalidContract(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing inputs", body: `{}`, want: "inputs"},
		{name: "removed roots", body: `{"inputs":{},"fileRoots":{}}`, want: "unknown"},
		{name: "unknown field", body: `{"inputs":{},"extra":true}`, want: "unknown"},
		{name: "duplicate key", body: `{"inputs":{},"inputs":{}}`, want: "duplicate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(name, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRequest(name); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("ReadRequest() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseLauncherOutputPreservesSuccessfulEnvelope(t *testing.T) {
	input := []byte(`{
	  "ok": true,
	  "jobId": "job",
	  "capability": "game/status",
	  "ruleId": "game.exe",
	  "output": {"status": "ok"},
	  "provenance": []
	}`)
	output, err := parseLauncherOutput(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["capability"] != "game/status" {
		t.Fatalf("output = %#v", decoded)
	}
}

func TestParseLauncherOutputRequiresErrorOnFailedExit(t *testing.T) {
	if _, err := parseLauncherOutput(
		[]byte(`{"ok":false,"error":"owning Rule mismatch"}`),
		errors.New("exit status 1"),
	); err == nil || !strings.Contains(err.Error(), "owning Rule mismatch") {
		t.Fatalf("parseLauncherOutput() error = %v", err)
	}
}
