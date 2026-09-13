package sftpruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	RuntimeID      = "windows-sftp-v1"
	RequiredUser   = "windowsagent"
	Authentication = "none"
)

type Config struct {
	Listen       string
	StatusListen string
	HostKeyFile  string
	Logger       *slog.Logger
}

type Server struct {
	config      Config
	sshConfig   *ssh.ServerConfig
	fingerprint string
	logger      *slog.Logger
	ready       chan struct{}
	readyOnce   sync.Once

	mu             sync.RWMutex
	sftpListen     string
	sshListener    net.Listener
	statusListener net.Listener
	connections    map[net.Conn]struct{}
	connectionWG   sync.WaitGroup
}

func New(config Config) (*Server, error) {
	if config.Listen == "" {
		return nil, errors.New("SFTP listen address is required")
	}
	if config.StatusListen == "" {
		return nil, errors.New("status listen address is required")
	}
	if err := validateLoopbackListen(config.StatusListen); err != nil {
		return nil, err
	}
	signer, err := loadHostKey(config.HostKeyFile)
	if err != nil {
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	sshConfig := &ssh.ServerConfig{
		NoClientAuth: true,
		NoClientAuthCallback: func(metadata ssh.ConnMetadata) (*ssh.Permissions, error) {
			if metadata.User() != RequiredUser {
				return nil, fmt.Errorf("SSH username must be %q", RequiredUser)
			}
			return nil, nil
		},
		ServerVersion: "SSH-2.0-WindowsAgent_SFTP",
	}
	sshConfig.AddHostKey(signer)
	return &Server{
		config:      config,
		sshConfig:   sshConfig,
		fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		logger:      logger,
		ready:       make(chan struct{}),
		connections: make(map[net.Conn]struct{}),
	}, nil
}

func (s *Server) Ready() <-chan struct{} { return s.ready }

func (s *Server) SFTPListen() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sftpListen
}

func (s *Server) StatusListen() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.statusListener == nil {
		return ""
	}
	return s.statusListener.Addr().String()
}

func (s *Server) HostKeyFingerprint() string { return s.fingerprint }

func (s *Server) Serve(ctx context.Context) error {
	sshListener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return fmt.Errorf("listen for SFTP on %s: %w", s.config.Listen, err)
	}
	statusListener, err := net.Listen("tcp", s.config.StatusListen)
	if err != nil {
		_ = sshListener.Close()
		return fmt.Errorf("listen for SFTP status on %s: %w", s.config.StatusListen, err)
	}

	s.mu.Lock()
	s.sftpListen = sshListener.Addr().String()
	s.sshListener = sshListener
	s.statusListener = statusListener
	s.mu.Unlock()

	statusServer := &http.Server{Handler: s.statusHandler()}
	acceptError := make(chan error, 1)
	statusError := make(chan error, 1)
	go func() { acceptError <- s.acceptConnections(sshListener) }()
	go func() { statusError <- statusServer.Serve(statusListener) }()
	s.readyOnce.Do(func() { close(s.ready) })
	s.logger.Info("sftp_runtime_started",
		"runtime", RuntimeID,
		"sftp_listen", s.SFTPListen(),
		"status_listen", statusListener.Addr().String(),
		"username", RequiredUser,
		"client_authentication", Authentication,
		"host_key_fingerprint", s.fingerprint,
	)

	var serveErr error
	acceptStopped := false
	select {
	case <-ctx.Done():
	case err := <-acceptError:
		acceptStopped = true
		if err != nil && !errors.Is(err, net.ErrClosed) {
			serveErr = fmt.Errorf("accept SFTP connection: %w", err)
		}
	case err := <-statusError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			serveErr = fmt.Errorf("serve SFTP status: %w", err)
		}
	}

	_ = sshListener.Close()
	if !acceptStopped {
		<-acceptError
	}
	_ = statusServer.Close()
	_ = statusListener.Close()
	s.closeConnections()
	s.connectionWG.Wait()
	s.logger.Info("sftp_runtime_stopped", "runtime", RuntimeID)
	return serveErr
}

func (s *Server) acceptConnections(listener net.Listener) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.connections[connection] = struct{}{}
		s.mu.Unlock()
		s.connectionWG.Add(1)
		go func() {
			defer s.connectionWG.Done()
			defer func() {
				s.mu.Lock()
				delete(s.connections, connection)
				s.mu.Unlock()
				_ = connection.Close()
			}()
			s.serveConnection(connection)
		}()
	}
}

func (s *Server) serveConnection(connection net.Conn) {
	remote := connection.RemoteAddr().String()
	serverConnection, channels, globalRequests, err := ssh.NewServerConn(connection, s.sshConfig)
	if err != nil {
		s.logger.Warn("sftp_ssh_handshake_failed", "remote", remote, "error", err)
		return
	}
	defer serverConnection.Close()
	s.logger.Info("sftp_client_connected", "remote", remote, "username", serverConnection.User())
	defer s.logger.Info("sftp_client_disconnected", "remote", remote, "username", serverConnection.User())

	go rejectGlobalRequests(globalRequests, s.logger, remote)
	var channelWG sync.WaitGroup
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			s.logger.Warn("sftp_channel_rejected", "remote", remote, "channel_type", newChannel.ChannelType())
			_ = newChannel.Reject(ssh.Prohibited, "only session channels carrying the sftp subsystem are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			s.logger.Warn("sftp_session_accept_failed", "remote", remote, "error", err)
			continue
		}
		channelWG.Add(1)
		go func() {
			defer channelWG.Done()
			s.serveSession(channel, requests, remote)
		}()
	}
	channelWG.Wait()
}

func (s *Server) serveSession(channel ssh.Channel, requests <-chan *ssh.Request, remote string) {
	defer channel.Close()
	for request := range requests {
		if request.Type != "subsystem" {
			s.logger.Warn("sftp_session_request_rejected", "remote", remote, "request_type", request.Type)
			_ = request.Reply(false, nil)
			continue
		}
		var payload struct{ Name string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil || payload.Name != "sftp" {
			s.logger.Warn("sftp_subsystem_rejected", "remote", remote, "subsystem", payload.Name)
			_ = request.Reply(false, nil)
			continue
		}
		if err := request.Reply(true, nil); err != nil {
			s.logger.Warn("sftp_subsystem_reply_failed", "remote", remote, "error", err)
			return
		}
		server, err := sftp.NewServer(channel, sftp.WindowsRootEnumeratesDrives())
		if err != nil {
			s.logger.Error("sftp_subsystem_initialization_failed", "remote", remote, "error", err)
			return
		}
		s.logger.Info("sftp_subsystem_started", "remote", remote)
		if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
			s.logger.Warn("sftp_subsystem_failed", "remote", remote, "error", err)
		}
		_ = server.Close()
		return
	}
}

func rejectGlobalRequests(requests <-chan *ssh.Request, logger *slog.Logger, remote string) {
	for request := range requests {
		logger.Warn("sftp_global_request_rejected", "remote", remote, "request_type", request.Type)
		_ = request.Reply(false, nil)
	}
}

func (s *Server) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for connection := range s.connections {
		_ = connection.Close()
	}
}

func (s *Server) statusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(struct {
			Status             string `json:"status"`
			Runtime            string `json:"runtime"`
			SFTPListen         string `json:"sftpListen"`
			Username           string `json:"username"`
			ClientAuth         string `json:"clientAuthentication"`
			HostKeyFingerprint string `json:"hostKeyFingerprint"`
		}{
			Status:             "ok",
			Runtime:            RuntimeID,
			SFTPListen:         s.SFTPListen(),
			Username:           RequiredUser,
			ClientAuth:         Authentication,
			HostKeyFingerprint: s.fingerprint,
		})
	})
	return mux
}

func validateLoopbackListen(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid status listen address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("status listen address %q must use an explicit loopback IP", address)
	}
	return nil
}
