package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

const (
	stateDisabled = "DISABLED"
	stateStarting = "STARTING"
	stateOnline   = "ONLINE"
	stateStopping = "STOPPING"
	stateFailed   = "FAILED"
)

type config struct {
	DataDir     string
	StatusFile  string
	StopFile    string
	Listen      string
	ForwardTo   string
	Hostname    string
	AuthTimeout time.Duration
}

type status struct {
	SchemaVersion int       `json:"schemaVersion"`
	Enabled       bool      `json:"enabled"`
	State         string    `json:"state"`
	IPv4          string    `json:"ipv4,omitempty"`
	IPv6          string    `json:"ipv6,omitempty"`
	Error         string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ProcessID     int       `json:"processId"`
	Generation    string    `json:"generation"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "[FATAL]", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader) (runErr error) {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	writer := statusWriter{path: cfg.StatusFile}
	generation, err := newGeneration()
	if err != nil {
		return err
	}
	baseStatus := status{SchemaVersion: 1, Enabled: true, ProcessID: os.Getpid(), Generation: generation}
	startingStatus := baseStatus
	startingStatus.State, startingStatus.UpdatedAt = stateStarting, time.Now().UTC()
	if err := writer.write(startingStatus); err != nil {
		return fmt.Errorf("write starting status: %w", err)
	}
	defer func() {
		if runErr != nil {
			failedStatus := baseStatus
			failedStatus.State, failedStatus.Error, failedStatus.UpdatedAt = stateFailed, runErr.Error(), time.Now().UTC()
			_ = writer.write(failedStatus)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	stopWatcherDone := make(chan struct{})
	go watchStopFile(ctx, cfg.StopFile, stop, stopWatcherDone)
	defer func() {
		stop()
		<-stopWatcherDone
	}()
	authKey, err := readAuthKey(stdin)
	if err != nil {
		return err
	}
	stateStore, err := mem.New(nil, "windowsagent-assist")
	if err != nil {
		return fmt.Errorf("create in-memory Tailscale state: %w", err)
	}
	server := &tsnet.Server{Dir: cfg.DataDir, Store: stateStore, Hostname: cfg.Hostname, AuthKey: authKey, Ephemeral: true}
	defer server.Close()
	enrolled, loggedOut := false, false
	defer func() {
		if !enrolled || loggedOut {
			return
		}
		if err := logout(server); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	authContext, authCancel := context.WithTimeout(ctx, cfg.AuthTimeout)
	upStatus, err := server.Up(authContext)
	authCancel()
	if err != nil {
		return fmt.Errorf("connect ephemeral Tailscale node: %w", err)
	}
	enrolled = true
	ipv4, ipv6 := selectIPs(upStatus.TailscaleIPs)
	if !ipv4.IsValid() && !ipv6.IsValid() {
		return errors.New("Tailscale node is running without an assigned IP")
	}
	listener, err := server.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on tailnet %s: %w", cfg.Listen, err)
	}
	defer listener.Close()
	onlineStatus := baseStatus
	onlineStatus.State, onlineStatus.IPv4, onlineStatus.IPv6, onlineStatus.UpdatedAt = stateOnline, validString(ipv4), validString(ipv6), time.Now().UTC()
	if err := writer.write(onlineStatus); err != nil {
		return fmt.Errorf("write online status: %w", err)
	}
	serveError := make(chan error, 1)
	go func() { serveError <- serve(ctx, listener, cfg.ForwardTo) }()
	select {
	case err := <-serveError:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	case <-ctx.Done():
	}
	stoppingStatus := onlineStatus
	stoppingStatus.State, stoppingStatus.UpdatedAt = stateStopping, time.Now().UTC()
	if err := writer.write(stoppingStatus); err != nil {
		return fmt.Errorf("write stopping status: %w", err)
	}
	_ = listener.Close()
	if err := logout(server); err != nil {
		return err
	}
	loggedOut = true
	disabledStatus := baseStatus
	disabledStatus.Enabled, disabledStatus.State, disabledStatus.UpdatedAt = false, stateDisabled, time.Now().UTC()
	if err := writer.write(disabledStatus); err != nil {
		return fmt.Errorf("write disabled status: %w", err)
	}
	return nil
}

func logout(server *tsnet.Server) error {
	logoutContext, logoutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer logoutCancel()
	client, err := server.LocalClient()
	if err != nil {
		return fmt.Errorf("open Tailscale local client for logout: %w", err)
	}
	if err := client.Logout(logoutContext); err != nil {
		return fmt.Errorf("logout ephemeral Tailscale node: %w", err)
	}
	return nil
}

func newGeneration() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate adapter identity: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("windows-tailscale-adapter", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg config
	flags.StringVar(&cfg.DataDir, "data-dir", "", "absolute private adapter data directory")
	flags.StringVar(&cfg.StatusFile, "status-file", "", "absolute adapter status JSON path")
	flags.StringVar(&cfg.StopFile, "stop-file", "", "absolute local stop-request file")
	flags.StringVar(&cfg.Listen, "listen", ":8787", "tailnet-only TCP listen address")
	flags.StringVar(&cfg.ForwardTo, "forward-to", "127.0.0.1:8787", "explicit loopback WindowsAgent address")
	flags.StringVar(&cfg.Hostname, "hostname", "windowsagent-assist", "temporary tailnet hostname")
	flags.DurationVar(&cfg.AuthTimeout, "auth-timeout", time.Minute, "maximum node enrollment duration")
	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if cfg.DataDir == "" || !filepath.IsAbs(cfg.DataDir) {
		return config{}, errors.New("--data-dir must be an absolute path")
	}
	if cfg.StatusFile == "" {
		cfg.StatusFile = filepath.Join(cfg.DataDir, "status.json")
	} else if !filepath.IsAbs(cfg.StatusFile) {
		return config{}, errors.New("--status-file must be an absolute path")
	}
	if cfg.StopFile == "" {
		cfg.StopFile = filepath.Join(cfg.DataDir, "stop.request")
	} else if !filepath.IsAbs(cfg.StopFile) {
		return config{}, errors.New("--stop-file must be an absolute path")
	}
	if err := os.Remove(cfg.StopFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return config{}, fmt.Errorf("clear stale stop request: %w", err)
	}
	if cfg.Listen == "" {
		return config{}, errors.New("--listen is required")
	}
	host, port, err := net.SplitHostPort(cfg.ForwardTo)
	if err != nil || port == "" {
		return config{}, fmt.Errorf("--forward-to must be an explicit loopback host and port: %q", cfg.ForwardTo)
	}
	forwardIP, err := netip.ParseAddr(host)
	if err != nil || !forwardIP.IsLoopback() {
		return config{}, fmt.Errorf("--forward-to must use an explicit loopback IP: %q", cfg.ForwardTo)
	}
	if strings.TrimSpace(cfg.Hostname) == "" || strings.TrimSpace(cfg.Hostname) != cfg.Hostname {
		return config{}, errors.New("--hostname must be non-empty and canonical")
	}
	if cfg.AuthTimeout <= 0 {
		return config{}, errors.New("--auth-timeout must be positive")
	}
	return cfg, nil
}

func watchStopFile(ctx context.Context, path string, stop context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				_ = os.Remove(path)
				stop()
				return
			}
		}
	}
}

func readAuthKey(reader io.Reader) (string, error) {
	limited := io.LimitReader(reader, 4097)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read auth key from stdin: %w", err)
	}
	if len(data) > 4096 {
		return "", errors.New("auth key exceeds 4096 bytes")
	}
	key := strings.TrimSuffix(string(data), "\n")
	key = strings.TrimSuffix(key, "\r")
	if key == "" {
		return "", errors.New("auth key is required on stdin")
	}
	if strings.ContainsAny(key, "\r\n\t ") {
		return "", errors.New("auth key must not contain whitespace")
	}
	return key, nil
}

func selectIPs(addresses []netip.Addr) (ipv4, ipv6 netip.Addr) {
	for _, address := range addresses {
		if address.Is4() && !ipv4.IsValid() {
			ipv4 = address
		}
		if address.Is6() && !ipv6.IsValid() {
			ipv6 = address
		}
	}
	return ipv4, ipv6
}

func validString(address netip.Addr) string {
	if !address.IsValid() {
		return ""
	}
	return address.String()
}

func serve(ctx context.Context, listener net.Listener, forwardTo string) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go proxy(ctx, connection, forwardTo)
	}
}

func proxy(ctx context.Context, source net.Conn, forwardTo string) {
	defer source.Close()
	target, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", forwardTo)
	if err != nil {
		return
	}
	defer target.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = source.Close()
			_ = target.Close()
		case <-done:
		}
	}()
	var group sync.WaitGroup
	group.Add(2)
	go func() { defer group.Done(); _, _ = io.Copy(target, source) }()
	go func() { defer group.Done(); _, _ = io.Copy(source, target) }()
	group.Wait()
}

type statusWriter struct{ path string }

func (w statusWriter) write(value status) error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(w.path), ".status-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, w.path)
}
