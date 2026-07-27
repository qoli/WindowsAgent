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
	Listen         string
	DataDir        string
	RulesDir       string
	CaptureTimeout time.Duration
	Retention      int
	LogLevel       slog.Level
	LogFile        string
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
