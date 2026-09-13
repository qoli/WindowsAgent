package sftpruntime

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// InitializeHostKey creates one new Ed25519 SSH host key. It never overwrites
// an existing key and does not create a missing parent directory.
func InitializeHostKey(path string) error {
	if path == "" {
		return errors.New("host key path is required")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate Ed25519 host key: %w", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode Ed25519 host key: %w", err)
	}
	contents := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if len(contents) == 0 {
		return errors.New("encode Ed25519 host key: empty PEM output")
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create host key %q: %w", path, err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return fmt.Errorf("write host key %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync host key %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close host key %q: %w", path, err)
	}
	complete = true
	return nil
}

func loadHostKey(path string) (ssh.Signer, error) {
	if path == "" {
		return nil, errors.New("host key path is required")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read host key %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(contents)
	if err != nil {
		return nil, fmt.Errorf("parse host key %q: %w", path, err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return nil, fmt.Errorf("parse host key %q: key type must be %s", path, ssh.KeyAlgoED25519)
	}
	return signer, nil
}
