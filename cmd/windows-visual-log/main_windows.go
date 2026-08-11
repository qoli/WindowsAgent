//go:build windows && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

	"github.com/qoli/WindowsAgent/internal/eventclient"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/frametap"
	"github.com/qoli/WindowsAgent/internal/visuallog"
	"github.com/qoli/WindowsAgent/internal/visualloghttp"
)

type commandConfig struct {
	ConfigFile       string
	ModelBaseURL     string
	ModelKeyFile     string
	EventBaseURL     string
	EventTokenFile   string
	ControlListen    string
	ControlTokenFile string
	LogFile          string
	StatusFile       string
	ModelTimeout     time.Duration
	Once             bool
	ValidateOnly     bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	manifest, err := visuallog.LoadConfig(cfg.ConfigFile)
	if err != nil {
		return err
	}
	modelKey, err := readSecret(cfg.ModelKeyFile, 1)
	if err != nil {
		return fmt.Errorf("read model API key: %w", err)
	}
	eventHTTP := boundedHTTPClient(10 * time.Second)
	events, err := eventclient.New(cfg.EventBaseURL, cfg.EventTokenFile, eventHTTP)
	if err != nil {
		return fmt.Errorf("initialize event client: %w", err)
	}
	modelClient, err := visuallog.NewModelClient(cfg.ModelBaseURL, modelKey, boundedHTTPClient(cfg.ModelTimeout), manifest)
	if err != nil {
		return err
	}
	if cfg.ValidateOnly {
		if _, err := readSecret(cfg.ControlTokenFile, 32); err != nil {
			return fmt.Errorf("read visual log control token: %w", err)
		}
		return nil
	}
	tap, err := frametap.OpenReader(manifest.Evidence.FrameTapName)
	if err != nil {
		return err
	}
	defer tap.Close()
	frameSource, err := visuallog.NewFrameTapSource(tap, manifest.TargetExecutable, manifest.MaxFrameAge())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
		return fmt.Errorf("create visual log directory: %w", err)
	}
	logOutput, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open visual log process log: %w", err)
	}
	defer logOutput.Close()
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	defer func() {
		if runErr != nil {
			logger.Error("visual_log_failed", "error", runErr.Error())
			_ = visuallog.WriteJSONAtomic(cfg.StatusFile, status(manifest, "failed", "", 0, runErr.Error()))
		}
	}()
	healthContext, healthCancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = events.Health(healthContext)
	healthCancel()
	if err != nil {
		return fmt.Errorf("require event stream: %w", err)
	}
	sessionID, err := visuallog.NewIdentity("vlogsession")
	if err != nil {
		return err
	}
	instanceID, err := visuallog.NewIdentity("vloginstance")
	if err != nil {
		return err
	}
	runner := visuallog.Runner{
		Config: manifest, Frames: frameSource, Describer: modelClient, Events: events,
		SessionID: sessionID, InstanceID: instanceID,
		OnCommitted: func(event eventstream.Event) {
			logger.Info("visual_log_observation_committed", "sequence", event.Sequence, "observed_at", event.ObservedAt)
			_ = visuallog.WriteJSONAtomic(cfg.StatusFile, status(manifest, "active", event.SessionID, event.Sequence, ""))
		},
		OnDropped: func(sample visuallog.DroppedSample) {
			logger.Warn("visual_log_sample_dropped", "stage", sample.Stage, "capture_id", sample.CaptureID, "error", sample.Cause.Error())
		},
	}
	logger.Info("visual_log_process_started", "module_id", manifest.ModuleID, "target_executable", manifest.TargetExecutable,
		"model", manifest.Model.ID, "interval_ms", manifest.IntervalMS, "warmup_calls", manifest.WarmupCalls)
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Once {
		if err := visuallog.WriteJSONAtomic(cfg.StatusFile, status(manifest, "warming", sessionID, 0, "")); err != nil {
			return err
		}
		if err := runner.RunOnce(signalContext); err != nil {
			return err
		}
	} else {
		controlToken, err := readSecret(cfg.ControlTokenFile, 32)
		if err != nil {
			return fmt.Errorf("read visual log control token: %w", err)
		}
		controller, err := visuallog.NewController(signalContext, runner)
		if err != nil {
			return err
		}
		defer controller.Close()
		controlAPI, err := visualloghttp.New(controller, controlToken)
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", cfg.ControlListen)
		if err != nil {
			return fmt.Errorf("listen on visual log control %s: %w", cfg.ControlListen, err)
		}
		defer listener.Close()
		server := &http.Server{
			Handler: controlAPI.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
			WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
			ErrorLog: log.New(&slogWriter{logger: logger}, "", 0),
		}
		if err := visuallog.WriteJSONAtomic(cfg.StatusFile, status(manifest, "idle", "", 0, "")); err != nil {
			return err
		}
		logger.Info("visual_log_control_started", "listen", listener.Addr().String())
		serveError := make(chan error, 1)
		go func() { serveError <- server.Serve(listener) }()
		select {
		case err := <-serveError:
			if !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve visual log control: %w", err)
			}
		case <-signalContext.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownContext); err != nil {
				return fmt.Errorf("shutdown visual log control: %w", err)
			}
			if err := <-serveError; err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve visual log control during shutdown: %w", err)
			}
		}
	}
	logger.Info("visual_log_process_stopped", "reason", "cancellation_or_once")
	return visuallog.WriteJSONAtomic(cfg.StatusFile, status(manifest, "stopped", sessionID, 0, ""))
}

func parseFlags(args []string) (commandConfig, error) {
	flags := flag.NewFlagSet("windows-visual-log", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg commandConfig
	flags.StringVar(&cfg.ConfigFile, "config", "", "absolute per-Game visual log config path")
	flags.StringVar(&cfg.ModelBaseURL, "model-base-url", "", "oMLX OpenAI-compatible LAN base URL")
	flags.StringVar(&cfg.ModelKeyFile, "model-api-key-file", "", "absolute model API key file")
	flags.StringVar(&cfg.EventBaseURL, "event-base-url", "http://127.0.0.1:8788", "loopback event API origin")
	flags.StringVar(&cfg.EventTokenFile, "event-token-file", "", "absolute event API token file")
	flags.StringVar(&cfg.ControlListen, "control-listen", "127.0.0.1:8789", "loopback visual log control listen address")
	flags.StringVar(&cfg.ControlTokenFile, "control-token-file", "", "absolute visual log control bearer token file")
	flags.StringVar(&cfg.LogFile, "log-file", "", "absolute process JSONL log path")
	flags.StringVar(&cfg.StatusFile, "status-file", "", "absolute atomically replaced status path")
	flags.DurationVar(&cfg.ModelTimeout, "model-timeout", 30*time.Second, "maximum duration of one oMLX request")
	flags.BoolVar(&cfg.Once, "once", false, "warm the model, commit one description, and exit")
	flags.BoolVar(&cfg.ValidateOnly, "validate-only", false, "validate config and secrets without contacting runtimes")
	if err := flags.Parse(args); err != nil {
		return commandConfig{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return commandConfig{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	for name, value := range map[string]string{
		"--config": cfg.ConfigFile, "--model-api-key-file": cfg.ModelKeyFile, "--event-token-file": cfg.EventTokenFile,
		"--control-token-file": cfg.ControlTokenFile, "--log-file": cfg.LogFile, "--status-file": cfg.StatusFile,
	} {
		if value == "" || !filepath.IsAbs(value) {
			return commandConfig{}, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	if cfg.ModelTimeout <= 0 || cfg.ModelTimeout > 5*time.Minute {
		return commandConfig{}, errors.New("--model-timeout must be greater than zero and at most five minutes")
	}
	if err := validateLoopbackListen(cfg.ControlListen); err != nil {
		return commandConfig{}, err
	}
	return cfg, nil
}

func validateLoopbackListen(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return errors.New("--control-listen must be an explicit loopback IP and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("--control-listen must use an explicit loopback IP")
	}
	return nil
}

func boundedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: false, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
	}}
}

func readSecret(name string, minimum int64) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < minimum || info.Size() > 4096 {
		return "", fmt.Errorf("secret file must be regular and between %d and 4096 bytes", minimum)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	secret := string(data)
	if strings.TrimSpace(secret) != secret {
		return "", errors.New("secret file must not contain leading or trailing whitespace")
	}
	return secret, nil
}

func status(config visuallog.Config, state, sessionID string, sequence uint64, failure string) map[string]any {
	value := map[string]any{
		"schemaVersion": 1, "updatedAt": time.Now().UTC(), "state": state, "moduleId": config.ModuleID,
		"targetExecutable": config.TargetExecutable, "model": config.Model.ID, "sessionId": sessionID,
		"lastSequence": sequence,
	}
	if failure != "" {
		value["error"] = failure
	}
	return value
}

type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(data []byte) (int, error) {
	w.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
