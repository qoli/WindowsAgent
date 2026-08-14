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

	"github.com/qoli/WindowsAgent/internal/actionosd"
	"github.com/qoli/WindowsAgent/internal/captureindicator"
	"github.com/qoli/WindowsAgent/internal/eventclient"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/eventweb"
	"github.com/qoli/WindowsAgent/internal/recordingindicator"
)

const streamRetryDelay = time.Second

type config struct {
	Listen             string
	EventAPIURL        string
	EventTokenFile     string
	WebTokenFile       string
	LogFile            string
	MinimumEventCursor uint64
}

type localIndicators struct{}

func (localIndicators) CaptureActive() (bool, error)   { return captureindicator.Active() }
func (localIndicators) RecordingActive() (bool, error) { return recordingindicator.Active() }

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "windows-event-web:", err)
		os.Exit(1)
	}
	logger, closeLog, err := newLogger(cfg.LogFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "windows-event-web:", err)
		os.Exit(1)
	}
	defer closeLog()
	if err := run(cfg, logger); err != nil {
		logger.Error("event_web_failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, logger *slog.Logger) error {
	httpClient := &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}}
	source, err := eventclient.New(cfg.EventAPIURL, cfg.EventTokenFile, httpClient)
	if err != nil {
		return fmt.Errorf("initialize event client: %w", err)
	}
	webToken, err := readToken(cfg.WebTokenFile, "web")
	if err != nil {
		return err
	}
	projection := &eventweb.Projection{}
	startupContext, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	after, err := reconstruct(startupContext, source, projection, cfg.MinimumEventCursor)
	if err != nil {
		return err
	}
	projection.SetConnection(true, after)
	web, err := eventweb.New(source, projection, localIndicators{}, webToken)
	if err != nil {
		return fmt.Errorf("initialize event Web interface: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           web.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          log.New(&slogWriter{logger: logger}, "", 0),
	}

	runContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	streamFatal := make(chan error, 1)
	go followProjection(runContext, source, projection, after, logger, streamFatal)
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	logger.Info("event_web_started", "listen", listener.Addr().String(), "event_api_url", cfg.EventAPIURL,
		"after_cursor", after, "minimum_event_cursor", cfg.MinimumEventCursor)

	var terminal error
	serveCompleted := false
	select {
	case err := <-serveError:
		serveCompleted = true
		if !errors.Is(err, http.ErrServerClosed) {
			terminal = fmt.Errorf("serve event Web interface: %w", err)
		}
	case err := <-streamFatal:
		terminal = err
	case <-runContext.Done():
	}
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil && terminal == nil {
		terminal = fmt.Errorf("shutdown event Web interface: %w", err)
	}
	if !serveCompleted {
		select {
		case err := <-serveError:
			if err != nil && !errors.Is(err, http.ErrServerClosed) && terminal == nil {
				terminal = fmt.Errorf("serve event Web interface during shutdown: %w", err)
			}
		case <-time.After(10 * time.Second):
			if terminal == nil {
				terminal = errors.New("event Web interface did not stop before deadline")
			}
		}
	}
	if terminal != nil {
		return terminal
	}
	logger.Info("event_web_stopped")
	return nil
}

func reconstruct(ctx context.Context, source *eventclient.Client, projection *eventweb.Projection, minimum uint64) (uint64, error) {
	if err := source.Health(ctx); err != nil {
		return 0, fmt.Errorf("require event service: %w", err)
	}
	last, err := source.LastSequence(ctx)
	if err != nil {
		return 0, fmt.Errorf("resolve current event cursor: %w", err)
	}
	after, err := actionosd.StartupCursor(last, minimum, eventstream.DefaultReplayLimit)
	if err != nil {
		return 0, err
	}
	for after < last {
		events, next, currentLast, err := source.Replay(ctx, after, eventstream.DefaultReplayLimit)
		if err != nil {
			return 0, fmt.Errorf("reconstruct Web OSD state: %w", err)
		}
		for _, event := range events {
			if err := projection.Apply(event); err != nil {
				return 0, fmt.Errorf("reconstruct Web OSD event %d: %w", event.Sequence, err)
			}
		}
		if next <= after {
			return 0, errors.New("event replay did not advance its cursor")
		}
		after, last = next, currentLast
	}
	projection.SetConnection(true, after)
	return after, nil
}

func followProjection(ctx context.Context, source *eventclient.Client, projection *eventweb.Projection, after uint64, logger *slog.Logger, fatal chan<- error) {
	retryCount := 0
	for ctx.Err() == nil {
		healthContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		healthErr := source.Health(healthContext)
		cancel()
		if healthErr != nil {
			projection.SetConnection(false, after)
		} else {
			projection.SetConnection(true, after)
			var applyErr error
			streamErr := source.Stream(ctx, after, func(event eventstream.Event) error {
				if err := projection.Apply(event); err != nil {
					applyErr = fmt.Errorf("project event %d: %w", event.Sequence, err)
					return applyErr
				}
				after = event.Sequence
				if retryCount > 0 {
					logger.Info("event_web_stream_reconnected", "sequence", event.Sequence, "retry_count", retryCount)
				}
				retryCount = 0
				return nil
			})
			projection.SetConnection(false, after)
			if applyErr != nil {
				select {
				case fatal <- applyErr:
				default:
				}
				return
			}
			if ctx.Err() != nil {
				return
			}
			healthErr = streamErr
		}
		retryCount++
		logger.Warn("event_web_stream_disconnected", "error", healthErr, "after_cursor", after,
			"retry_count", retryCount, "retry_in_ms", streamRetryDelay.Milliseconds())
		timer := time.NewTimer(streamRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func parseConfig(arguments []string) (config, error) {
	flags := flag.NewFlagSet("windows-event-web", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg config
	flags.StringVar(&cfg.Listen, "listen", "127.0.0.1:8790", "loopback Web HTTP listen address")
	flags.StringVar(&cfg.EventAPIURL, "event-api-url", "http://127.0.0.1:8788", "loopback windows-event-stream origin")
	flags.StringVar(&cfg.EventTokenFile, "event-token-file", "", "absolute event-stream bearer token file")
	flags.StringVar(&cfg.WebTokenFile, "web-token-file", "", "absolute browser-facing bearer token file")
	flags.StringVar(&cfg.LogFile, "log-file", "", "absolute JSON log file")
	flags.Uint64Var(&cfg.MinimumEventCursor, "minimum-event-cursor", 0, "explicit lower bound for OSD reconstruction")
	if err := flags.Parse(arguments); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if err := eventweb.ValidateListenAddress(cfg.Listen); err != nil {
		return config{}, err
	}
	for label, name := range map[string]string{
		"--event-token-file": cfg.EventTokenFile, "--web-token-file": cfg.WebTokenFile, "--log-file": cfg.LogFile,
	} {
		if name == "" || !filepath.IsAbs(name) {
			return config{}, fmt.Errorf("%s must be an absolute path", label)
		}
	}
	return cfg, nil
}

func readToken(name, label string) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", fmt.Errorf("stat %s token file: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 4096 {
		return "", fmt.Errorf("%s token file must be a regular file between 32 and 4096 bytes", label)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read %s token file: %w", label, err)
	}
	token := string(data)
	if strings.TrimSpace(token) != token {
		return "", fmt.Errorf("%s token file must not contain leading or trailing whitespace", label)
	}
	return token, nil
}

func newLogger(name string) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create Web log directory: %w", err)
	}
	output, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open Web log: %w", err)
	}
	return slog.New(slog.NewJSONHandler(output, nil)), func() { _ = output.Close() }, nil
}

type slogWriter struct{ logger *slog.Logger }

func (w *slogWriter) Write(data []byte) (int, error) {
	w.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
