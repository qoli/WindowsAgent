//go:build windows && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/watchdog"
)

type config struct {
	ConfigFile   string
	StatusFile   string
	LogFile      string
	ValidateOnly bool
	Once         bool
}

func main() {
	if err := run(os.Args[1:], os.Getenv("LOCALAPPDATA")); err != nil {
		fmt.Fprintln(os.Stderr, "windows-watchdog:", err)
		os.Exit(1)
	}
}

func run(args []string, localAppData string) error {
	cfg, err := parseFlags(args, localAppData)
	if err != nil {
		return err
	}
	watchdogConfig, err := watchdog.LoadConfig(cfg.ConfigFile)
	if err != nil {
		return err
	}
	if cfg.ValidateOnly {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
		return fmt.Errorf("create watchdog log directory: %w", err)
	}
	logFile, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open watchdog log: %w", err)
	}
	defer logFile.Close()
	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	httpClient := &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}}
	observer, err := watchdog.NewTargetObserver(httpClient, watchdog.WindowsProcessInspector{})
	if err != nil {
		return err
	}
	engine, err := watchdog.NewEngine(watchdogConfig, observer, watchdog.WindowsTaskRecoverer{},
		watchdog.FileStatusSink{Name: cfg.StatusFile}, logger)
	if err != nil {
		return err
	}
	logger.Info("watchdog_started", "config_file", cfg.ConfigFile, "status_file", cfg.StatusFile,
		"target_count", len(watchdogConfig.Targets), "self_recovery", false)
	if cfg.Once {
		if err := engine.Cycle(context.Background()); err != nil {
			return err
		}
		logger.Info("watchdog_once_completed")
		return nil
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := engine.Run(signalContext); err != nil {
		return err
	}
	logger.Info("watchdog_stopped")
	return nil
}

func parseFlags(args []string, localAppData string) (config, error) {
	if localAppData == "" {
		return config{}, errors.New("LOCALAPPDATA is required")
	}
	root := filepath.Join(localAppData, "gameGuide", "windows-capture-agent", "watchdog")
	flags := flag.NewFlagSet("windows-watchdog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg config
	flags.StringVar(&cfg.ConfigFile, "config", filepath.Join(root, "config.json"), "absolute watchdog target configuration file")
	flags.StringVar(&cfg.StatusFile, "status-file", filepath.Join(root, "status.json"), "absolute atomically replaced watchdog status file")
	flags.StringVar(&cfg.LogFile, "log-file", filepath.Join(root, "watchdog.jsonl"), "absolute watchdog JSON log file")
	flags.BoolVar(&cfg.ValidateOnly, "validate-only", false, "validate configuration and exit without observing or recovering targets")
	flags.BoolVar(&cfg.Once, "once", false, "run exactly one observation and recovery cycle")
	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	for label, value := range map[string]string{"--config": cfg.ConfigFile, "--status-file": cfg.StatusFile, "--log-file": cfg.LogFile} {
		if value == "" || !filepath.IsAbs(value) {
			return config{}, fmt.Errorf("%s must be an absolute path", label)
		}
	}
	if cfg.ValidateOnly && cfg.Once {
		return config{}, errors.New("--validate-only and --once are mutually exclusive")
	}
	return cfg, nil
}
