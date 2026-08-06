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

	"github.com/qoli/WindowsAgent/internal/eventhttp"
	"github.com/qoli/WindowsAgent/internal/eventstream"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("windows-event-stream", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var listen, dataDir, tokenFile, logFile string
	flags.StringVar(&listen, "listen", "127.0.0.1:8788", "loopback HTTP listen address")
	flags.StringVar(&dataDir, "data-dir", "", "absolute event journal data directory")
	flags.StringVar(&tokenFile, "token-file", "", "absolute file containing the local event API bearer token")
	flags.StringVar(&logFile, "log-file", "", "absolute JSON log file")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return errors.New("--data-dir must be an absolute path")
	}
	if tokenFile == "" || !filepath.IsAbs(tokenFile) {
		return errors.New("--token-file must be an absolute path")
	}
	if logFile == "" || !filepath.IsAbs(logFile) {
		return errors.New("--log-file must be an absolute path")
	}
	if err := validateLoopbackListen(listen); err != nil {
		return err
	}
	token, err := readToken(tokenFile)
	if err != nil {
		return err
	}
	store, err := eventstream.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open event journal: %w", err)
	}
	defer store.Close()
	api, err := eventhttp.New(store, token)
	if err != nil {
		return fmt.Errorf("initialize event API: %w", err)
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	defer listener.Close()
	if err := os.MkdirAll(filepath.Dir(logFile), 0o700); err != nil {
		return fmt.Errorf("create event log directory: %w", err)
	}
	logOutput, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event log file: %w", err)
	}
	defer logOutput.Close()
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	server := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// A live NDJSON response intentionally remains idle between committed
		// events. Per-response write deadlines would terminate valid subscribers.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     log.New(&slogWriter{logger: logger}, "", 0),
	}
	last, err := store.LastSequence()
	if err != nil {
		return err
	}
	logger.Info("event_stream_started", "listen", listener.Addr().String(), "root", store.Root(), "last_sequence", last)

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case err := <-serveError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve event API: %w", err)
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown event API: %w", err)
		}
		err := <-serveError
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve event API during shutdown: %w", err)
		}
		logger.Info("event_stream_stopped")
		return nil
	}
}

func validateLoopbackListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return fmt.Errorf("--listen must be a loopback host and port: %q", listen)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen must use an explicit loopback IP address: %q", listen)
	}
	return nil
}

func readToken(name string) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", fmt.Errorf("stat event API token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 4096 {
		return "", errors.New("event API token file must be a regular file between 32 and 4096 bytes")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read event API token file: %w", err)
	}
	token := string(data)
	if strings.TrimSpace(token) != token {
		return "", errors.New("event API token file must not contain leading or trailing whitespace")
	}
	return token, nil
}

type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(data []byte) (int, error) {
	w.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
