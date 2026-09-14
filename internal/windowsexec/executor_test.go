package windowsexec

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateRequestDefaultsWindowByOperation(t *testing.T) {
	tests := []struct {
		request Request
		want    WindowMode
	}{
		{Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe"}, WindowHidden},
		{Request{SchemaVersion: 1, Operation: OperationStart, Executable: "tool.exe"}, WindowNormal},
		{Request{SchemaVersion: 1, Operation: OperationPowerShellFile, ScriptPath: `C:\work\task.ps1`}, WindowHidden},
	}
	for _, test := range tests {
		got, err := validateRequest(test.request)
		if err != nil {
			t.Fatalf("validate %s: %v", test.request.Operation, err)
		}
		if got.Window != test.want {
			t.Fatalf("validate %s window = %q, want %q", test.request.Operation, got.Window, test.want)
		}
	}
}

func TestValidateRequestRejectsInvalidOperationFields(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		code string
	}{
		{"schema version", Request{Operation: OperationRun, Executable: "tool.exe"}, "EXEC_INVALID_SCHEMA_VERSION"},
		{"unknown operation", Request{SchemaVersion: 1, Operation: "shell", Executable: "cmd.exe"}, "EXEC_INVALID_OPERATION"},
		{"run missing executable", Request{SchemaVersion: 1, Operation: OperationRun}, "EXEC_INVALID_EXECUTABLE"},
		{"run with script", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", ScriptPath: "task.ps1"}, "EXEC_INVALID_SCRIPT_PATH"},
		{"powershell missing script", Request{SchemaVersion: 1, Operation: OperationPowerShellFile}, "EXEC_INVALID_SCRIPT_PATH"},
		{"powershell relative script", Request{SchemaVersion: 1, Operation: OperationPowerShellFile, ScriptPath: "task.ps1"}, "EXEC_INVALID_SCRIPT_PATH"},
		{"powershell wrong extension", Request{SchemaVersion: 1, Operation: OperationPowerShellFile, ScriptPath: `C:\work\task.txt`}, "EXEC_INVALID_SCRIPT_PATH"},
		{"powershell executable override", Request{SchemaVersion: 1, Operation: OperationPowerShellFile, ScriptPath: `C:\task.ps1`, Executable: "pwsh.exe"}, "EXEC_INVALID_EXECUTABLE"},
		{"start stdin", Request{SchemaVersion: 1, Operation: OperationStart, Executable: "tool.exe", Stdin: []byte("input")}, "EXEC_INVALID_STDIN"},
		{"start output limit", Request{SchemaVersion: 1, Operation: OperationStart, Executable: "tool.exe", MaxOutputBytes: 1}, "EXEC_INVALID_OUTPUT_LIMIT"},
		{"start timeout", Request{SchemaVersion: 1, Operation: OperationStart, Executable: "tool.exe", TimeoutMilliseconds: 1}, "EXEC_INVALID_TIMEOUT"},
		{"relative cwd", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", Cwd: "work"}, "EXEC_INVALID_CWD"},
		{"invalid window", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", Window: "minimized"}, "EXEC_INVALID_WINDOW"},
		{"nul argument", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", Argv: []string{"bad\x00arg"}}, "EXEC_INVALID_ARGUMENT"},
		{"invalid env name", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", Env: map[string]string{"A=B": "x"}}, "EXEC_INVALID_ENVIRONMENT"},
		{"case-colliding env names", Request{SchemaVersion: 1, Operation: OperationRun, Executable: "tool.exe", Env: map[string]string{"Path": "a", "PATH": "b"}}, "EXEC_INVALID_ENVIRONMENT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateRequest(test.req)
			var execErr *Error
			if !errors.As(err, &execErr) || execErr.Code != test.code {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestValidateUsesWindowsPathSemanticsOnEveryHost(t *testing.T) {
	valid := []string{`C:\work\task`, `D:/work/task`, `\\server\share\task`}
	for _, path := range valid {
		if !isAbsoluteWindowsPath(path) {
			t.Errorf("path %q was not absolute", path)
		}
	}
	invalid := []string{"", `work\task`, `C:task`, `\\server`, `\\server\`}
	for _, path := range invalid {
		if isAbsoluteWindowsPath(path) {
			t.Errorf("path %q was absolute", path)
		}
	}
}

func TestPowerShellArgumentsUseFileModeWithoutCommandString(t *testing.T) {
	got := powerShellArguments(`C:\work folder\task.ps1`, []string{"-Mode", "Repair Now"})
	want := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", `C:\work folder\task.ps1`, "-Mode", "Repair Now"}
	if len(got) != len(want) {
		t.Fatalf("arguments = %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("argument %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestEncodeOutputPreservesBytesAndOnlyValidUTF8Text(t *testing.T) {
	valid := encodeOutput([]byte("hello"))
	if valid.BytesBase64 != "aGVsbG8=" || valid.Text == nil || *valid.Text != "hello" || valid.ByteLength != 5 {
		t.Fatalf("valid output = %#v", valid)
	}
	invalid := encodeOutput([]byte{0xff, 0x00})
	if invalid.BytesBase64 != "/wA=" || invalid.Text != nil || invalid.ByteLength != 2 {
		t.Fatalf("invalid output = %#v", invalid)
	}
}

func TestOutputJSONKeepsEmptyUTF8Text(t *testing.T) {
	encoded, err := json.Marshal(encodeOutput(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"bytesBase64":"","text":"","byteLength":0}` {
		t.Fatalf("json = %s", encoded)
	}
}

func TestUnsupportedExecutorStillValidatesRequest(t *testing.T) {
	_, err := (OSExecutor{}).Execute(context.Background(), Request{SchemaVersion: 1, Operation: "shell"})
	var execErr *Error
	if !errors.As(err, &execErr) || execErr.Code != "EXEC_INVALID_OPERATION" {
		t.Fatalf("error = %v", err)
	}
}
