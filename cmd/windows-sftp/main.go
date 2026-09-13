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

	"github.com/qoli/WindowsAgent/internal/sftpruntime"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "host-key" {
		return runHostKey(args[1:])
	}
	flags := flag.NewFlagSet("windows-sftp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var listen, statusListen, hostKeyFile, logFile string
	flags.StringVar(&listen, "listen", "0.0.0.0:2022", "SFTP listen address")
	flags.StringVar(&statusListen, "status-listen", "127.0.0.1:8793", "loopback readiness listen address")
	flags.StringVar(&hostKeyFile, "host-key-file", "", "absolute persistent SSH host key path")
	flags.StringVar(&logFile, "log-file", "", "absolute structured JSON log path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if hostKeyFile == "" || !filepath.IsAbs(hostKeyFile) {
		return errors.New("--host-key-file must be an absolute path")
	}
	if logFile == "" || !filepath.IsAbs(logFile) {
		return errors.New("--log-file must be an absolute path")
	}
	logOutput, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open SFTP log file: %w", err)
	}
	defer logOutput.Close()
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	server, err := sftpruntime.New(sftpruntime.Config{
		Listen:       listen,
		StatusListen: statusListen,
		HostKeyFile:  hostKeyFile,
		Logger:       logger,
	})
	if err != nil {
		return fmt.Errorf("initialize SFTP runtime: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Serve(ctx); err != nil {
		return fmt.Errorf("serve SFTP runtime: %w", err)
	}
	return nil
}

func runHostKey(args []string) error {
	if len(args) == 0 || args[0] != "init" {
		return errors.New("host-key requires the init subcommand")
	}
	flags := flag.NewFlagSet("windows-sftp host-key init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var path string
	flags.StringVar(&path, "path", "", "absolute host key output path")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("parse host-key init flags: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected host-key init arguments: %s", strings.Join(flags.Args(), " "))
	}
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("host-key init --path must be an absolute path")
	}
	if err := sftpruntime.InitializeHostKey(path); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, path)
	return nil
}
