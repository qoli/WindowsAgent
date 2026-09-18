package main

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigRequiresLoopbackForwardTarget(t *testing.T) {
	_, err := parseConfig([]string{"--data-dir", t.TempDir(), "--forward-to", "0.0.0.0:8787"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if _, err := parseConfig([]string{"--data-dir", t.TempDir(), "--auth-timeout", time.Second.String()}); err != nil {
		t.Fatalf("parseConfig(valid) error = %v", err)
	}
}

func TestReadAuthKeyIsBoundedAndCanonical(t *testing.T) {
	key, err := readAuthKey(strings.NewReader("tskey-auth-test\n"))
	if err != nil || key != "tskey-auth-test" {
		t.Fatalf("readAuthKey() = %q, %v", key, err)
	}
	if _, err := readAuthKey(strings.NewReader("bad key")); err == nil {
		t.Fatal("readAuthKey() accepted whitespace")
	}
	if _, err := readAuthKey(strings.NewReader(strings.Repeat("x", 4097))); err == nil {
		t.Fatal("readAuthKey() accepted oversized input")
	}
}

func TestSelectIPs(t *testing.T) {
	ipv4, ipv6 := selectIPs([]netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1"), netip.MustParseAddr("100.64.0.1")})
	if ipv4.String() != "100.64.0.1" || ipv6.String() != "fd7a:115c:a1e0::1" {
		t.Fatalf("selectIPs() = %s, %s", ipv4, ipv6)
	}
}

func TestWatchStopFileCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopPath := filepath.Join(t.TempDir(), "stop.request")
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go watchStopFile(ctx, stopPath, func() { close(done); cancel() }, watchDone)
	if err := os.WriteFile(stopPath, []byte("stop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchStopFile did not cancel")
	}
	<-watchDone
}
