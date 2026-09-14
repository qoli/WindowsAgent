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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/delegatedhttp"
	"github.com/qoli/WindowsAgent/internal/delegatedtask"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/piweb"
)

type config struct {
	listen    string
	dataDir   string
	tokenFile string
	piWebURL  string
	logFile   string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "windows-agent-pi:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	cfg, err := parseConfig(arguments)
	if err != nil {
		return err
	}
	logger, closeLog, err := newLogger(cfg.logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	httpClient := &http.Client{Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2: false, MaxIdleConns: 16, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
	}}
	runtime, err := piweb.New(cfg.piWebURL, httpClient)
	if err != nil {
		return fmt.Errorf("initialize PI WEB adapter: %w", err)
	}
	journal, err := eventstream.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("open delegated task journal: %w", err)
	}
	defer journal.Close()
	manager, err := delegatedtask.NewManager(journal, runtime)
	if err != nil {
		return fmt.Errorf("initialize delegated task manager: %w", err)
	}
	defer manager.Close()
	token, err := readToken(cfg.tokenFile)
	if err != nil {
		return err
	}
	api, err := delegatedhttp.New(manager, token)
	if err != nil {
		return fmt.Errorf("initialize delegated task API: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.listen, err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 0, IdleTimeout: 60 * time.Second,
		ErrorLog: log.New(&slogWriter{logger: logger}, "", 0),
	}
	logger.Info("windows_agent_pi_started", "listen", listener.Addr().String(), "pi_web_url", cfg.piWebURL, "data_dir", journal.Root())
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case err := <-serveError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve delegated task API: %w", err)
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown delegated task API: %w", err)
		}
		if err := <-serveError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve delegated task API during shutdown: %w", err)
		}
		logger.Info("windows_agent_pi_stopped")
		return nil
	}
}

func parseConfig(arguments []string) (config, error) {
	flags := flag.NewFlagSet("windows-agent-pi", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg config
	flags.StringVar(&cfg.listen, "listen", "127.0.0.1:8791", "loopback delegated task API listen address")
	flags.StringVar(&cfg.dataDir, "data-dir", "", "absolute delegated task journal directory")
	flags.StringVar(&cfg.tokenFile, "token-file", "", "absolute bearer-token file")
	flags.StringVar(&cfg.piWebURL, "pi-web-url", "http://127.0.0.1:8504", "loopback PI WEB server URL")
	flags.StringVar(&cfg.logFile, "log-file", "", "absolute JSON runtime log file")
	if err := flags.Parse(arguments); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if err := validateLoopbackListen(cfg.listen); err != nil {
		return config{}, err
	}
	for name, value := range map[string]string{"--data-dir": cfg.dataDir, "--token-file": cfg.tokenFile, "--log-file": cfg.logFile} {
		if value == "" || !filepath.IsAbs(value) {
			return config{}, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	parsed, err := url.Parse(cfg.piWebURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return config{}, errors.New("--pi-web-url must be an absolute http URL")
	}
	return cfg, nil
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
		return "", fmt.Errorf("stat delegated task API token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 4096 {
		return "", errors.New("delegated task API token file must be a regular file between 32 and 4096 bytes")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read delegated task API token file: %w", err)
	}
	token := string(data)
	if strings.TrimSpace(token) != token {
		return "", errors.New("delegated task API token file must not contain leading or trailing whitespace")
	}
	return token, nil
}

func newLogger(name string) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	return slog.New(slog.NewJSONHandler(file, nil)), func() { _ = file.Close() }, nil
}

type slogWriter struct{ logger *slog.Logger }

func (writer *slogWriter) Write(data []byte) (int, error) {
	writer.logger.Error("http_server_error", "message", strings.TrimSpace(string(data)))
	return len(data), nil
}
