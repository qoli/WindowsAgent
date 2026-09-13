package sftpruntime

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestInitializeAndLoadHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-key.pem")
	if err := InitializeHostKey(path); err != nil {
		t.Fatalf("InitializeHostKey: %v", err)
	}
	signer, err := loadHostKey(path)
	if err != nil {
		t.Fatalf("loadHostKey: %v", err)
	}
	if got := signer.PublicKey().Type(); got != "ssh-ed25519" {
		t.Fatalf("key type = %q, want ssh-ed25519", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("key permissions = %#o, want 0600", got)
	}
	firstFingerprint := ssh.FingerprintSHA256(signer.PublicKey())
	reloaded, err := loadHostKey(path)
	if err != nil {
		t.Fatalf("reload host key: %v", err)
	}
	if got := ssh.FingerprintSHA256(reloaded.PublicKey()); got != firstFingerprint {
		t.Fatalf("reloaded fingerprint = %q, want %q", got, firstFingerprint)
	}
}

func TestInitializeHostKeyRefusesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-key.pem")
	if err := InitializeHostKey(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeHostKey(path); err == nil {
		t.Fatal("InitializeHostKey unexpectedly overwrote existing key")
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatal("existing host key changed")
	}
}

func TestInitializeHostKeyDoesNotCreateParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	if err := InitializeHostKey(filepath.Join(parent, "host-key.pem")); err == nil {
		t.Fatal("InitializeHostKey unexpectedly created a missing parent")
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("missing parent stat error = %v, want not exist", err)
	}
}

func TestLoadHostKeyFailsForMissingAndMalformedFiles(t *testing.T) {
	if _, err := loadHostKey(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("loadHostKey unexpectedly accepted a missing key")
	}
	path := filepath.Join(t.TempDir(), "malformed")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHostKey(path); err == nil {
		t.Fatal("loadHostKey unexpectedly accepted malformed key")
	}
}

func TestLoadHostKeyRejectsNonEd25519Key(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rsa-key")
	contents := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHostKey(path); err == nil {
		t.Fatal("loadHostKey unexpectedly accepted a non-Ed25519 key")
	}
}
