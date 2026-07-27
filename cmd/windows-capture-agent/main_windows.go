//go:build windows && amd64

package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/artifact"
	"github.com/qoli/WindowsAgent/internal/config"
	"github.com/qoli/WindowsAgent/internal/httpapi"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/wgc"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	cfg, err := config.Parse(os.Args[1:], os.Getenv("LOCALAPPDATA"))
	if err != nil {
		return err
	}
	logOutput := os.Stdout
	var logFile *os.File
	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
			return fmt.Errorf("create log directory: %w", err)
		}
		logFile, err = os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer logFile.Close()
		logOutput = logFile
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: cfg.LogLevel}))
	defer func() {
		if runErr != nil {
			logger.Error("capture_agent_failed", "error", runErr)
		}
	}()
	logger.Warn("unauthenticated_lan_listener",
		"listen", cfg.Listen,
		"warning", "any device that can reach this address can trigger and download screenshots",
	)

	store, err := artifact.New(filepath.Join(cfg.DataDir, "captures"), cfg.Retention)
	if err != nil {
		return fmt.Errorf("initialize artifact store: %w", err)
	}
	capturer, err := wgc.New(logger)
	if err != nil {
		return fmt.Errorf("initialize WGC capturer: %w", err)
	}
	ruleStore, err := rules.New(cfg.RulesDir)
	if err != nil {
		return fmt.Errorf("initialize rule store: %w", err)
	}
	scriptToken, err := readScriptAPIToken(cfg.ScriptTokenFile)
	if err != nil {
		return fmt.Errorf("read Script API token: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	scriptExecutor, err := scriptlaunch.NewLocalExecutor(filepath.Dir(executable), cfg.RulesDir)
	if err != nil {
		return fmt.Errorf("initialize local Script executor: %w", err)
	}
	api, err := httpapi.New(
		capturer,
		store,
		ruleStore,
		scriptExecutor,
		scriptToken,
		cfg.CaptureTimeout,
		version,
		logger,
	)
	if err != nil {
		return fmt.Errorf("initialize HTTP API: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.New(&slogWriter{logger: logger}, "", 0),
	}
	logger.Info("capture_agent_started",
		"version", version,
		"listen", listener.Addr().String(),
		"artifact_root", store.Root(),
		"retention", cfg.Retention,
		"capture_timeout", cfg.CaptureTimeout.String(),
		"rules_root", ruleStore.Root(),
		"script_api_auth", "bearer-token",
	)

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() {
		serveError <- server.Serve(listener)
	}()

	select {
	case err := <-serveError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-signalContext.Done():
		logger.Info("capture_agent_stopping")
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		err := <-serveError
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		logger.Info("capture_agent_stopped")
		return nil
	}
}

func readScriptAPIToken(name string) (string, error) {
	if name == "" || !filepath.IsAbs(name) {
		return "", errors.New("absolute Script API token file is required")
	}
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Script API token file must be a regular file")
	}
	if info.Size() != 64 {
		return "", errors.New("Script API token must contain exactly 64 hexadecimal characters")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	if _, err := hex.DecodeString(string(data)); err != nil {
		return "", fmt.Errorf("Script API token is not hexadecimal: %w", err)
	}
	return string(data), nil
}

type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(data []byte) (int, error) {
	w.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
