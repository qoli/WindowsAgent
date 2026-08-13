package wgcworker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
)

type fakeWorkerClient struct {
	pid         int
	statusCalls int
	fullCalls   int
	regionCalls int
	closeCalls  int
	fullErr     error
	regionErr   error
}

type fakeCaptureNotifier struct {
	pulses int
	err    error
}

func (n *fakeCaptureNotifier) Pulse() error {
	n.pulses++
	return n.err
}

func (c *fakeWorkerClient) PID() int { return c.pid }

func (c *fakeWorkerClient) Status(context.Context, time.Time) (capture.Status, error) {
	c.statusCalls++
	return capture.Status{Supported: true}, nil
}

func (c *fakeWorkerClient) Capture(context.Context, time.Time, capture.Request) (capture.Result, error) {
	c.fullCalls++
	if c.fullErr != nil {
		return capture.Result{}, c.fullErr
	}
	return capture.Result{Profile: capture.ProfileNativeJPEG}, nil
}

func (c *fakeWorkerClient) CaptureRegion(context.Context, time.Time, capture.RegionRequest) (capture.RegionResult, error) {
	c.regionCalls++
	if c.regionErr != nil {
		return capture.RegionResult{}, c.regionErr
	}
	return capture.RegionResult{ImageWidth: 10, ImageHeight: 10}, nil
}

func (c *fakeWorkerClient) Close() error {
	c.closeCalls++
	return nil
}

func workerFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "windows-wgc-worker.exe")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCapturerReusesOneHealthyWorkerGeneration(t *testing.T) {
	client := &fakeWorkerClient{pid: 41}
	notifier := &fakeCaptureNotifier{}
	starts := 0
	capturer, err := newWithStarter(context.Background(), workerFixture(t), time.Second, true, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) {
			starts++
			return client, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capturer.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := capturer.Capture(context.Background(), capture.Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := capturer.CaptureRegion(context.Background(), capture.RegionRequest{}); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || client.statusCalls != 1 || client.fullCalls != 1 || client.regionCalls != 1 || notifier.pulses != 2 {
		t.Fatalf("starts=%d status=%d full=%d region=%d", starts, client.statusCalls, client.fullCalls, client.regionCalls)
	}
	if err := capturer.Close(); err != nil {
		t.Fatal(err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("close calls=%d", client.closeCalls)
	}
}

func TestCapturerDoesNotReplayDomainFailureAndStartsNextGenerationLater(t *testing.T) {
	first := &fakeWorkerClient{pid: 41, fullErr: capture.Failure("capture_readback_failed", "D3D11 Map failed", errors.New("worker exited"))}
	second := &fakeWorkerClient{pid: 42}
	clients := []workerClient{first, second}
	var mu sync.Mutex
	starts := 0
	capturer, err := newWithStarter(context.Background(), workerFixture(t), time.Second, false, &fakeCaptureNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) {
			mu.Lock()
			defer mu.Unlock()
			client := clients[starts]
			starts++
			return client, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_, err = capturer.Capture(context.Background(), capture.Request{})
	var failure *capture.Error
	if !errors.As(err, &failure) || failure.Code != "capture_readback_failed" {
		t.Fatalf("error=%v", err)
	}
	if starts != 1 || first.fullCalls != 1 || first.closeCalls != 1 || second.fullCalls != 0 {
		t.Fatalf("failed call was replayed: starts=%d first=%d/%d second=%d", starts, first.fullCalls, first.closeCalls, second.fullCalls)
	}
	if _, err := capturer.Capture(context.Background(), capture.Request{}); err != nil {
		t.Fatal(err)
	}
	if starts != 2 || second.fullCalls != 1 {
		t.Fatalf("next generation not used: starts=%d second=%d", starts, second.fullCalls)
	}
}

func TestCapturerRetriesRegionTransportEOFAcrossWorkerGenerations(t *testing.T) {
	first := &fakeWorkerClient{pid: 41, regionErr: fmt.Errorf("read frame header: %w", io.EOF)}
	second := &fakeWorkerClient{pid: 42}
	clients := []workerClient{first, second}
	starts := 0
	capturer, err := newWithStarter(context.Background(), workerFixture(t), time.Second, false, &fakeCaptureNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) {
			client := clients[starts]
			starts++
			return client, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	result, err := capturer.CaptureRegion(context.Background(), capture.RegionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if starts != 2 || first.regionCalls != 1 || first.closeCalls != 1 || second.regionCalls != 1 || result.ImageWidth != 10 {
		t.Fatalf("starts=%d first=%d/%d second=%d result=%+v", starts, first.regionCalls, first.closeCalls, second.regionCalls, result)
	}
}

func TestCapturerDoesNotRetryNonTransportRegionFailure(t *testing.T) {
	client := &fakeWorkerClient{pid: 41, regionErr: capture.Failure("capture_readback_failed", "D3D11 Map failed", errors.New("HRESULT"))}
	starts := 0
	capturer, err := newWithStarter(context.Background(), workerFixture(t), time.Second, false, &fakeCaptureNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) {
			starts++
			return client, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_, err = capturer.CaptureRegion(context.Background(), capture.RegionRequest{})
	if err == nil {
		t.Fatal("expected region failure")
	}
	if starts != 1 || client.regionCalls != 1 || client.closeCalls != 1 {
		t.Fatalf("non-transport failure was retried: starts=%d calls=%d closes=%d", starts, client.regionCalls, client.closeCalls)
	}
}

func TestCapturerRejectsInvalidTimeout(t *testing.T) {
	_, err := newWithStarter(context.Background(), workerFixture(t), MaxCallDuration+time.Second, false, &fakeCaptureNotifier{}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) {
			return &fakeWorkerClient{}, nil
		})
	if err == nil {
		t.Fatal("expected invalid timeout failure")
	}
}

func TestCapturerNotificationFailureDoesNotChangeCapture(t *testing.T) {
	client := &fakeWorkerClient{pid: 41}
	notifier := &fakeCaptureNotifier{err: errors.New("indicator unavailable")}
	capturer, err := newWithStarter(context.Background(), workerFixture(t), time.Second, false, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(context.Context, string, bool, *slog.Logger) (workerClient, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capturer.Capture(context.Background(), capture.Request{}); err != nil {
		t.Fatalf("capture failed because its optional notification failed: %v", err)
	}
	if notifier.pulses != 1 || client.fullCalls != 1 {
		t.Fatalf("pulses=%d full=%d", notifier.pulses, client.fullCalls)
	}
}
