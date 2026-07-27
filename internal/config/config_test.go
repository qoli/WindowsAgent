package config

// Configuration tests stay platform-independent so they can run on macOS.

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "LocalAppData")
	cfg, err := Parse(nil, root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:8787" ||
		cfg.DataDir != filepath.Join(root, "gameGuide", "windows-capture-agent") ||
		cfg.RulesDir != filepath.Join(root, "gameGuide", "windows-capture-agent", "Rules") ||
		cfg.ScriptTokenFile != filepath.Join(root, "gameGuide", "windows-capture-agent", "script-api.token") ||
		cfg.CaptureTimeout != 5*time.Second ||
		cfg.Retention != 100 ||
		cfg.LogLevel != slog.LevelInfo ||
		cfg.LogFile != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestParseOverrides(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "capture-data")
	cfg, err := Parse([]string{
		"--listen", "127.0.0.1:9999",
		"--data-dir", root,
		"--rules-dir", filepath.Join(root, "external-rules"),
		"--script-api-token-file", filepath.Join(root, "script.token"),
		"--capture-timeout", "2s",
		"--retention", "7",
		"--log-level", "debug",
		"--log-file", filepath.Join(root, "agent.jsonl"),
	}, filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9999" ||
		cfg.DataDir != root ||
		cfg.RulesDir != filepath.Join(root, "external-rules") ||
		cfg.ScriptTokenFile != filepath.Join(root, "script.token") ||
		cfg.CaptureTimeout != 2*time.Second ||
		cfg.Retention != 7 ||
		cfg.LogLevel != slog.LevelDebug ||
		cfg.LogFile != filepath.Join(root, "agent.jsonl") {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestParseRejectsInvalidRequiredState(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		localAppData string
	}{
		{name: "missing local app data"},
		{name: "empty listen", args: []string{"--listen", ""}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "relative data dir", args: []string{"--data-dir", "relative"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "relative rules dir", args: []string{"--rules-dir", "relative"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "relative script token", args: []string{"--script-api-token-file", "token"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "zero timeout", args: []string{"--capture-timeout", "0s"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "zero retention", args: []string{"--retention", "0"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "unknown log level", args: []string{"--log-level", "verbose"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "relative log file", args: []string{"--log-file", "agent.jsonl"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
		{name: "positional argument", args: []string{"unexpected"}, localAppData: filepath.Join(string(filepath.Separator), "data")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.args, test.localAppData); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
