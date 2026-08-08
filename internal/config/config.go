// Package config owns process-level Windows Agent configuration.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Listen               string
	DataDir              string
	RulesDir             string
	CaptureTimeout       time.Duration
	Retention            int
	LogLevel             slog.Level
	LogFile              string
	RuntimeLogFile       string
	WGCTrace             bool
	OCRRuntimeRoot       string
	EventAPIURL          string
	EventTokenFile       string
	FrontierBindingsRoot string
}

func Parse(args []string, localAppData string) (Config, error) {
	if localAppData == "" {
		return Config{}, errors.New("LOCALAPPDATA is required")
	}
	defaultDataDir := filepath.Join(localAppData, "gameGuide", "windows-capture-agent")
	flagSet := flag.NewFlagSet("windows-capture-agent", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var cfg Config
	var level string
	flagSet.StringVar(&cfg.Listen, "listen", "0.0.0.0:8787", "HTTP listen address")
	flagSet.StringVar(&cfg.DataDir, "data-dir", defaultDataDir, "artifact data directory")
	flagSet.StringVar(&cfg.RulesDir, "rules-dir", "", "absolute external Rule plugin directory; empty uses <data-dir>/Rules")
	flagSet.DurationVar(&cfg.CaptureTimeout, "capture-timeout", 5*time.Second, "maximum time for one capture")
	flagSet.IntVar(&cfg.Retention, "retention", 100, "maximum number of screenshot artifacts")
	flagSet.StringVar(&level, "log-level", "info", "debug, info, warn, or error")
	flagSet.StringVar(&cfg.LogFile, "log-file", "", "absolute path for persistent JSON logs; empty writes to stdout")
	flagSet.StringVar(&cfg.RuntimeLogFile, "runtime-log-file", "", "absolute path for Go runtime and fatal stderr logs; empty preserves stderr")
	flagSet.BoolVar(&cfg.WGCTrace, "wgc-trace", false, "log every WGC capture operation lifecycle at info level")
	flagSet.StringVar(&cfg.OCRRuntimeRoot, "ocr-runtime-root", "", "absolute resident w480 OCR runtime bundle root; empty uses <data-dir>/runtimes/ppocr-w480")
	flagSet.StringVar(&cfg.EventAPIURL, "event-api-url", "http://127.0.0.1:8788", "loopback windows-event-stream HTTP origin")
	flagSet.StringVar(&cfg.EventTokenFile, "event-token-file", "", "absolute windows-event-stream token file; empty uses <data-dir>/event-api.token")
	flagSet.StringVar(&cfg.FrontierBindingsRoot, "frontier-bindings-root", "", "absolute Elite Dangerous bindings directory; empty uses LOCALAPPDATA/Frontier Developments/Elite Dangerous/Options/Bindings")
	if err := flagSet.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flagSet.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flagSet.Args(), " "))
	}
	if cfg.Listen == "" {
		return Config{}, errors.New("--listen is required")
	}
	if cfg.DataDir == "" || !filepath.IsAbs(cfg.DataDir) {
		return Config{}, errors.New("--data-dir must be an absolute path")
	}
	if cfg.RulesDir == "" {
		cfg.RulesDir = filepath.Join(cfg.DataDir, "Rules")
	} else if !filepath.IsAbs(cfg.RulesDir) {
		return Config{}, errors.New("--rules-dir must be empty or an absolute path")
	}
	if cfg.CaptureTimeout <= 0 {
		return Config{}, errors.New("--capture-timeout must be positive")
	}
	if cfg.Retention < 1 {
		return Config{}, errors.New("--retention must be at least 1")
	}
	if cfg.LogFile != "" && !filepath.IsAbs(cfg.LogFile) {
		return Config{}, errors.New("--log-file must be empty or an absolute path")
	}
	if cfg.RuntimeLogFile != "" && !filepath.IsAbs(cfg.RuntimeLogFile) {
		return Config{}, errors.New("--runtime-log-file must be empty or an absolute path")
	}
	if cfg.OCRRuntimeRoot == "" {
		cfg.OCRRuntimeRoot = filepath.Join(cfg.DataDir, "runtimes", "ppocr-w480")
	} else if !filepath.IsAbs(cfg.OCRRuntimeRoot) {
		return Config{}, errors.New("--ocr-runtime-root must be empty or an absolute path")
	}
	if cfg.EventAPIURL == "" {
		return Config{}, errors.New("--event-api-url is required")
	}
	if cfg.EventTokenFile == "" {
		cfg.EventTokenFile = filepath.Join(cfg.DataDir, "event-api.token")
	} else if !filepath.IsAbs(cfg.EventTokenFile) {
		return Config{}, errors.New("--event-token-file must be empty or an absolute path")
	}
	if cfg.FrontierBindingsRoot == "" {
		cfg.FrontierBindingsRoot = filepath.Join(localAppData, "Frontier Developments", "Elite Dangerous", "Options", "Bindings")
	} else if !filepath.IsAbs(cfg.FrontierBindingsRoot) {
		return Config{}, errors.New("--frontier-bindings-root must be empty or an absolute path")
	}
	switch strings.ToLower(level) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return Config{}, fmt.Errorf("invalid --log-level %q", level)
	}
	return cfg, nil
}
