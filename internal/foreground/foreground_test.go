package foreground

import (
	"strings"
	"testing"
	"time"
)

func TestInfoValidate(t *testing.T) {
	valid := Info{
		ObservedAt:     time.Date(2026, 7, 27, 1, 2, 3, 4, time.UTC),
		ProcessID:      42,
		ExecutableName: "game.exe",
		ExecutablePath: `C:\Games\game.exe`,
		WindowTitle:    "Game",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid foreground info rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Info)
		want string
	}{
		{name: "missing observed time", edit: func(info *Info) { info.ObservedAt = time.Time{} }, want: "observed_at"},
		{name: "missing process ID", edit: func(info *Info) { info.ProcessID = 0 }, want: "process_id"},
		{name: "missing executable name", edit: func(info *Info) { info.ExecutableName = "" }, want: "executable_name"},
		{name: "missing executable path", edit: func(info *Info) { info.ExecutablePath = "" }, want: "executable_path"},
		{name: "mismatched executable name", edit: func(info *Info) { info.ExecutableName = "other.exe" }, want: "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := valid
			test.edit(&info)
			if err := info.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
