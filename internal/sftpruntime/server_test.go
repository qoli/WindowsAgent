package sftpruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestServerSFTPHealthAndRestrictedSSHSurface(t *testing.T) {
	server, cancel, serveError := startTestServer(t)
	defer func() {
		cancel()
		if err := <-serveError; err != nil {
			t.Errorf("Serve during shutdown: %v", err)
		}
	}()

	client := dialTestSSH(t, server.SFTPListen(), RequiredUser)
	defer client.Close()

	t.Run("SFTP reads and writes host filesystem", func(t *testing.T) {
		sftpClient, err := sftp.NewClient(client)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer sftpClient.Close()
		path := filepath.ToSlash(filepath.Join(t.TempDir(), "round-trip.txt"))
		file, err := sftpClient.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
		if err != nil {
			t.Fatalf("OpenFile: %v", err)
		}
		if _, err := file.Write([]byte("windows-agent-sftp")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		readFile, err := sftpClient.Open(path)
		if err != nil {
			t.Fatalf("Open for read: %v", err)
		}
		contents, err := io.ReadAll(readFile)
		_ = readFile.Close()
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if got := string(contents); got != "windows-agent-sftp" {
			t.Fatalf("contents = %q", got)
		}
	})

	t.Run("shell is rejected", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.Shell(); err == nil {
			t.Fatal("Shell unexpectedly succeeded")
		}
	})

	t.Run("exec is rejected", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.Run("whoami"); err == nil {
			t.Fatal("exec unexpectedly succeeded")
		}
	})

	t.Run("pty is rejected", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err == nil {
			t.Fatal("PTY unexpectedly succeeded")
		}
	})

	t.Run("other subsystem is rejected", func(t *testing.T) {
		session, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		if err := session.RequestSubsystem("netconf"); err == nil {
			t.Fatal("non-SFTP subsystem unexpectedly succeeded")
		}
	})

	t.Run("port forwarding is rejected", func(t *testing.T) {
		connection, err := client.Dial("tcp", "127.0.0.1:1")
		if err == nil {
			connection.Close()
			t.Fatal("direct-tcpip channel unexpectedly succeeded")
		}
	})

	t.Run("remote port forwarding is rejected", func(t *testing.T) {
		listener, err := client.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			listener.Close()
			t.Fatal("tcpip-forward request unexpectedly succeeded")
		}
	})

	t.Run("health identifies exact runtime", func(t *testing.T) {
		response, err := http.Get("http://" + server.StatusListen() + "/healthz")
		if err != nil {
			t.Fatalf("GET healthz: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", response.StatusCode)
		}
		var health struct {
			Status             string `json:"status"`
			Runtime            string `json:"runtime"`
			SFTPListen         string `json:"sftpListen"`
			Username           string `json:"username"`
			ClientAuth         string `json:"clientAuthentication"`
			HostKeyFingerprint string `json:"hostKeyFingerprint"`
		}
		if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
			t.Fatal(err)
		}
		if health.Status != "ok" || health.Runtime != RuntimeID || health.SFTPListen != server.SFTPListen() ||
			health.Username != RequiredUser || health.ClientAuth != Authentication ||
			health.HostKeyFingerprint != server.HostKeyFingerprint() {
			t.Fatalf("unexpected health: %+v", health)
		}
		if strings.Contains(strings.ToLower(health.HostKeyFingerprint), "private") {
			t.Fatalf("health leaked key material: %q", health.HostKeyFingerprint)
		}
	})
}

func TestServerRejectsWrongUsername(t *testing.T) {
	server, cancel, serveError := startTestServer(t)
	defer func() {
		cancel()
		if err := <-serveError; err != nil {
			t.Errorf("Serve during shutdown: %v", err)
		}
	}()
	config := testSSHConfig("someone-else")
	if client, err := ssh.Dial("tcp", server.SFTPListen(), config); err == nil {
		client.Close()
		t.Fatal("SSH handshake unexpectedly accepted wrong username")
	}
}

func TestServerCancellationClosesActiveConnection(t *testing.T) {
	server, cancel, serveError := startTestServer(t)
	client := dialTestSSH(t, server.SFTPListen(), RequiredUser)
	cancel()
	select {
	case err := <-serveError:
		if err != nil {
			t.Fatalf("Serve during cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop with an active SSH connection")
	}
	if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err == nil {
		client.Close()
		t.Fatal("active SSH connection remained usable after shutdown")
	}
	_ = client.Close()
}

func TestNewRejectsMissingStateAndNonLoopbackStatus(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := New(Config{Listen: "127.0.0.1:0", StatusListen: "127.0.0.1:0", HostKeyFile: filepath.Join(t.TempDir(), "missing"), Logger: logger}); err == nil {
		t.Fatal("New unexpectedly accepted missing host key")
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := InitializeHostKey(key); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Listen: "127.0.0.1:0", StatusListen: "0.0.0.0:8793", HostKeyFile: key, Logger: logger}); err == nil {
		t.Fatal("New unexpectedly accepted non-loopback status listener")
	}
	if _, err := New(Config{Listen: "127.0.0.1:0", StatusListen: "127.0.0.1:0", HostKeyFile: key}); err == nil {
		t.Fatal("New unexpectedly accepted nil logger")
	}
}

func TestNewConfiguresOnlyNoneAuthentication(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	if err := InitializeHostKey(key); err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{
		Listen:       "127.0.0.1:0",
		StatusListen: "127.0.0.1:0",
		HostKeyFile:  key,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !server.sshConfig.NoClientAuth || server.sshConfig.NoClientAuthCallback == nil {
		t.Fatal("none authentication is not configured")
	}
	if server.sshConfig.PasswordCallback != nil || server.sshConfig.PublicKeyCallback != nil || server.sshConfig.KeyboardInteractiveCallback != nil {
		t.Fatal("an undeclared client authentication method is configured")
	}
}

func TestServeFailsWhenListenerCannotBind(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	if err := InitializeHostKey(key); err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{
		Listen:       "127.0.0.1:0",
		StatusListen: "127.0.0.1:bad-port",
		HostKeyFile:  key,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(context.Background()); err == nil {
		t.Fatal("Serve unexpectedly succeeded with invalid status listener")
	}
}

func startTestServer(t *testing.T) (*Server, context.CancelFunc, <-chan error) {
	t.Helper()
	key := filepath.Join(t.TempDir(), "host-key")
	if err := InitializeHostKey(key); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	server, err := New(Config{
		Listen:       "127.0.0.1:0",
		StatusListen: "127.0.0.1:0",
		HostKeyFile:  key,
		Logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(ctx) }()
	select {
	case <-server.Ready():
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatalf("server did not become ready; logs=%s", logs.String())
	}
	return server, cancel, serveError
}

func dialTestSSH(t *testing.T, address, username string) *ssh.Client {
	t.Helper()
	client, err := ssh.Dial("tcp", address, testSSHConfig(username))
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	return client
}

func testSSHConfig(username string) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Test-only ephemeral server key.
		Timeout:         5 * time.Second,
	}
}
