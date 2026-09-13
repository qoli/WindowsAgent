package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qoli/WindowsAgent/internal/sftpruntime"
)

func TestRunHostKeyInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host-key")
	if err := run([]string{"host-key", "init", "--path", path}); err != nil {
		t.Fatalf("run: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "BEGIN PRIVATE KEY") {
		t.Fatal("initialized file is not a private key PEM")
	}
	if err := run([]string{"host-key", "init", "--path", path}); err == nil {
		t.Fatal("second init unexpectedly overwrote key")
	}
}

func TestRunRejectsInvalidCommandsAndPaths(t *testing.T) {
	tests := [][]string{
		{"host-key"},
		{"host-key", "show"},
		{"host-key", "init", "--path", "relative"},
		{"--host-key-file", "relative", "--log-file", filepath.Join(t.TempDir(), "log")},
		{"--host-key-file", filepath.Join(t.TempDir(), "key"), "--log-file", "relative"},
	}
	for _, args := range tests {
		if err := run(args); err == nil {
			t.Fatalf("run(%q) unexpectedly succeeded", args)
		}
	}
}

func TestConstantsMatchPublicCLIContract(t *testing.T) {
	if sftpruntime.RequiredUser != "windowsagent" || sftpruntime.Authentication != "none" {
		t.Fatalf("unexpected identity contract: %q/%q", sftpruntime.RequiredUser, sftpruntime.Authentication)
	}
}
