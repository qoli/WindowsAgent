//go:build windows && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/scenereducer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("windows-screen-scene-reducer", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var configPath, eventBaseURL, tokenFile, stateFile, logFile, statusFile string
	var validateOnly bool
	flags.StringVar(&configPath, "config", "", "absolute scene reducer manifest path")
	flags.StringVar(&eventBaseURL, "event-base-url", "http://127.0.0.1:8788", "loopback event API base URL")
	flags.StringVar(&tokenFile, "token-file", "", "absolute event API token path")
	flags.StringVar(&stateFile, "state-file", "", "absolute durable reducer state path")
	flags.StringVar(&logFile, "log-file", "", "absolute reducer JSONL process log path")
	flags.StringVar(&statusFile, "status-file", "", "absolute reducer status JSON path")
	flags.BoolVar(&validateOnly, "validate-only", false, "validate the runtime contract and exit")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	for name, value := range map[string]string{"--config": configPath, "--token-file": tokenFile, "--state-file": stateFile, "--log-file": logFile, "--status-file": statusFile} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	config, err := scenereducer.LoadConfig(configPath)
	if err != nil {
		return err
	}
	token, err := readCanonicalToken(tokenFile)
	if err != nil {
		return err
	}
	client, err := scenereducer.NewClient(eventBaseURL, token)
	if err != nil {
		return err
	}
	state, err := scenereducer.LoadState(stateFile, config)
	if err != nil {
		return err
	}
	if validateOnly {
		return nil
	}
	logOutput, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open reducer log: %w", err)
	}
	defer logOutput.Close()
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	logger.Info("scene_reducer_started", "module_id", config.ModuleID, "input_stream", config.Input.Stream, "output_stream", config.Output.Stream, "cursor", state.Cursor)

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := scenereducer.Runner{
		Config: config, Client: client, StatePath: stateFile,
		OnProgress: func(state scenereducer.State) error {
			status := map[string]any{
				"schemaVersion": 1, "updatedAt": time.Now().UTC(), "state": "active", "moduleId": config.ModuleID,
				"inputStream": config.Input.Stream, "outputStream": config.Output.Stream, "cursor": state.Cursor,
				"lastOutputSequence": state.LastOutputSequence, "pending": state.Pending != nil,
			}
			if state.Scene != nil {
				status["sceneSignature"] = state.Scene.Signature
				status["lastInputSequence"] = state.Scene.LastSequence
				status["detectionCount"] = state.Scene.DetectionCount
			}
			return scenereducer.WriteJSONAtomic(statusFile, status)
		},
	}
	if err := runner.Run(signalContext); err != nil {
		logger.Error("scene_reducer_failed", "error", err.Error())
		_ = scenereducer.WriteJSONAtomic(statusFile, map[string]any{"schemaVersion": 1, "updatedAt": time.Now().UTC(), "state": "failed", "moduleId": config.ModuleID, "error": err.Error()})
		return err
	}
	logger.Info("scene_reducer_stopped", "reason", "cancellation")
	return scenereducer.WriteJSONAtomic(statusFile, map[string]any{"schemaVersion": 1, "updatedAt": time.Now().UTC(), "state": "stopped", "moduleId": config.ModuleID})
}

func readCanonicalToken(name string) (string, error) {
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
