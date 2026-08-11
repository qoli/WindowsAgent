//go:build windows && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/evidence"
	"github.com/qoli/WindowsAgent/internal/evidencehttp"
)

type options struct{ config, dataDir, captureURL, listen, tokenFile string }
type statusTracker struct {
	mu    sync.Mutex
	value evidencehttp.Status
}

func (t *statusTracker) snapshot() evidencehttp.Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.value
}
func (t *statusTracker) commit(record evidence.Record) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.value.LastScheduledAt = record.ScheduledAt
	if record.Kind == "frame" {
		t.value.Frames++
	} else {
		t.value.Gaps++
		if record.Gap != nil {
			t.value.LastError = record.Gap.Error
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}
func run() error {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	config, err := evidence.LoadConfig(opts.config)
	if err != nil {
		return err
	}
	token, err := readToken(opts.tokenFile)
	if err != nil {
		return err
	}
	store, err := evidence.OpenStore(opts.dataDir)
	if err != nil {
		return err
	}
	client, err := evidence.NewCaptureClient(opts.captureURL, &http.Client{Timeout: config.CaptureTimeout()}, config.TargetExecutable)
	if err != nil {
		return err
	}
	tracker := &statusTracker{value: evidencehttp.Status{State: "recording", StartedAt: time.Now().UTC()}}
	api, err := evidencehttp.New(store, token, time.Duration(config.MaxRangeSeconds)*time.Second, tracker.snapshot)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", opts.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	recorder := evidence.Recorder{Config: config, Capture: client, Sink: store, OnCommitted: tracker.commit}
	errorsChannel := make(chan error, 2)
	go func() { errorsChannel <- recorder.Run(ctx) }()
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorsChannel <- err
	}()
	first := <-errorsChannel
	stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownContext)
	if first != nil {
		return first
	}
	return nil
}
func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("windows-evidence-recorder", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var o options
	flags.StringVar(&o.config, "config", "", "absolute evidence config")
	flags.StringVar(&o.dataDir, "data-dir", "", "absolute evidence data directory")
	flags.StringVar(&o.captureURL, "capture-base-url", "http://127.0.0.1:8787", "loopback capture API origin")
	flags.StringVar(&o.listen, "listen", "127.0.0.1:8792", "loopback evidence API listen address")
	flags.StringVar(&o.tokenFile, "token-file", "", "absolute bearer token file")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("positional arguments are not accepted")
	}
	for name, value := range map[string]string{"--config": o.config, "--data-dir": o.dataDir, "--token-file": o.tokenFile} {
		if !filepath.IsAbs(value) {
			return options{}, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	host, port, err := net.SplitHostPort(o.listen)
	if err != nil || port == "" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return options{}, errors.New("--listen must use an explicit loopback IP and port")
	}
	return o, nil
}
func readToken(name string) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 32 || info.Size() > 4096 {
		return "", errors.New("token file must be regular and between 32 and 4096 bytes")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\t ") {
		return "", errors.New("token must contain at least 32 non-whitespace characters")
	}
	return token, nil
}
