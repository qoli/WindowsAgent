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

	"github.com/qoli/WindowsAgent/internal/evidence"
	"github.com/qoli/WindowsAgent/internal/evidencehttp"
	"github.com/qoli/WindowsAgent/internal/frametap"
	"github.com/qoli/WindowsAgent/internal/mfvideo"
	"github.com/qoli/WindowsAgent/internal/recordingindicator"
	"github.com/qoli/WindowsAgent/internal/wgc"
)

type options struct{ config, dataDir, listen, tokenFile string }

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
	encoderFactory := evidence.EncoderFactoryFunc(func(path string, format evidence.VideoFormat) (evidence.SegmentEncoder, error) {
		return mfvideo.NewEncoder(path, mfvideo.Format{Width: format.Width, Height: format.Height, FramesPerSecond: format.FramesPerSecond, Bitrate: format.Bitrate})
	})
	store, err := evidence.OpenStore(opts.dataDir, config, encoderFactory)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	tap, err := frametap.CreatePublisher(config.FrameTap.Name)
	if err != nil {
		return err
	}
	defer tap.Close()
	capturer, err := wgc.New(logger)
	if err != nil {
		return err
	}
	recordingIndicator := recordingindicator.NewPublisher()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	recorder := evidence.Recorder{Config: config, Stream: capturer, Lifecycle: recordingIndicator, Sink: store, FrameTap: tap}
	controller, err := evidence.NewRunController(ctx, recorder)
	if err != nil {
		return err
	}
	defer controller.Close()
	api, err := evidencehttp.New(store, mfvideo.NewDecoder(), token, time.Duration(config.MaxRangeSeconds)*time.Second, controller)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", opts.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second}
	errorsChannel := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errorsChannel <- err
	}()
	select {
	case serveErr := <-errorsChannel:
		stop()
		return serveErr
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return <-errorsChannel
	}
}
func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("windows-evidence-recorder", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var o options
	flags.StringVar(&o.config, "config", "", "absolute evidence config")
	flags.StringVar(&o.dataDir, "data-dir", "", "absolute evidence data directory")
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
