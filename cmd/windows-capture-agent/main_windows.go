//go:build windows && amd64

package main

import (
	"context"
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

	"github.com/qoli/WindowsAgent/internal/actionlaunch"
	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/artifact"
	"github.com/qoli/WindowsAgent/internal/config"
	"github.com/qoli/WindowsAgent/internal/eventclient"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/httpapi"
	"github.com/qoli/WindowsAgent/internal/inputaction"
	"github.com/qoli/WindowsAgent/internal/ocrworker"
	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/rules"
	"github.com/qoli/WindowsAgent/internal/scriptlaunch"
	"github.com/qoli/WindowsAgent/internal/wgc"
	"github.com/qoli/WindowsAgent/internal/windowsinput"
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
	if _, err := configureRuntimeDiagnostics(cfg.RuntimeLogFile); err != nil {
		return fmt.Errorf("configure runtime diagnostics: %w", err)
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
	capturer.SetTrace(cfg.WGCTrace)
	ruleStore, err := rules.New(cfg.RulesDir)
	if err != nil {
		return fmt.Errorf("initialize rule store: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	observationExecutor, err := scriptlaunch.NewLocalExecutor(filepath.Dir(executable), cfg.RulesDir)
	if err != nil {
		return fmt.Errorf("initialize local Script executor: %w", err)
	}
	ocrManager, err := ocrworker.NewManager(ruleStore, cfg.OCRRuntimeRoot, logger)
	if err != nil {
		return fmt.Errorf("initialize resident OCR runtime manager: %w", err)
	}
	defer func() {
		if err := ocrManager.Close(); err != nil {
			logger.Error("ocr_runtime_shutdown_failed", "error", err)
		}
	}()
	inputController, err := inputaction.NewController(cfg.FrontierBindingsRoot, windowsinput.WindowsDriver{}, foreground.Snapshot)
	if err != nil {
		return fmt.Errorf("initialize Frontier key Action controller: %w", err)
	}
	defer func() {
		if err := inputController.Close(); err != nil {
			logger.Error("input_controller_shutdown_failed", "error", err)
		}
	}()
	pointerController, err := pointeraction.NewController(windowsinput.WindowsDriver{}, foreground.Snapshot)
	if err != nil {
		return fmt.Errorf("initialize pointer Action controller: %w", err)
	}
	actionExecutor, err := actionlaunch.New(ruleStore, observationExecutor, capturer, ocrManager, inputController, pointerController, foreground.Snapshot)
	if err != nil {
		return fmt.Errorf("initialize Action executor: %w", err)
	}
	eventHTTPClient := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}}
	eventJournal, err := eventclient.New(cfg.EventAPIURL, cfg.EventTokenFile, eventHTTPClient)
	if err != nil {
		return fmt.Errorf("initialize event journal client: %w", err)
	}
	healthContext, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = eventJournal.Health(healthContext)
	healthCancel()
	if err != nil {
		return fmt.Errorf("require event journal service: %w", err)
	}
	actionManager, err := actionrun.NewManager(ruleStore, actionExecutor, eventJournal, foreground.Snapshot, logger)
	if err != nil {
		return fmt.Errorf("initialize Action invocation manager: %w", err)
	}
	defer actionManager.Close()
	api, err := httpapi.New(
		capturer,
		store,
		ruleStore,
		actionExecutor,
		actionManager,
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
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.New(&slogWriter{logger: logger}, "", 0),
	}
	logger.Info("capture_agent_started",
		"process_id", os.Getpid(),
		"version", version,
		"listen", listener.Addr().String(),
		"artifact_root", store.Root(),
		"retention", cfg.Retention,
		"capture_timeout", cfg.CaptureTimeout.String(),
		"rules_root", ruleStore.Root(),
		"script_api_auth", "none",
		"event_api_url", cfg.EventAPIURL,
		"frontier_bindings_root", cfg.FrontierBindingsRoot,
		"runtime_stderr_log", cfg.RuntimeLogFile,
		"wgc_trace", cfg.WGCTrace,
	)

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go reconcileOCRRuntime(signalContext, ocrManager, logger)
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

func reconcileOCRRuntime(ctx context.Context, manager *ocrworker.Manager, logger *slog.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := foreground.Snapshot()
		if err != nil {
			logger.Error("ocr_runtime_foreground_failed", "error", err)
		} else if err := manager.Reconcile(ctx, info.ExecutableName); err != nil {
			logger.Error("ocr_runtime_reconcile_failed", "foreground", info.ExecutableName, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(data []byte) (int, error) {
	w.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
