//go:build windows

package wgcworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/observationprotocol"
	"golang.org/x/sys/windows"
)

type processClient struct {
	mu      sync.Mutex
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	conn    *observationprotocol.Conn
	stderr  *boundedBuffer
	nextID  uint64
	pid     int
	closed  bool
}

func startWorkerProcess(ctx context.Context, executable string, trace bool, logger *slog.Logger) (workerClient, error) {
	command := exec.Command(executable, "--parent-pid", strconv.Itoa(os.Getpid()))
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open WGC worker stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open WGC worker stdout: %w", err)
	}
	stderr := &boundedBuffer{limit: 256 << 10}
	command.Stderr = io.MultiWriter(os.Stderr, stderr)
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start WGC worker process: %w", err)
	}
	conn, err := observationprotocol.NewConn(stdout, stdin, MaxFrameBytes)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	client := &processClient{command: command, stdin: stdin, stdout: stdout, conn: conn, stderr: stderr, pid: command.Process.Pid}
	deadline, err := effectiveInitializationDeadline(ctx)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	callContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var initialized initializeResult
	if err := client.call(callContext, methodInitialize, initializeParams{ProtocolVersion: ProtocolVersion, Trace: trace, Deadline: deadline}, &initialized); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("initialize WGC worker: %w: %s", err, stderr.String())
	}
	if err := validateInitializeResult(initialized, client.pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	logger.Info("wgc_worker_initialized",
		"process_id", initialized.ProcessID,
		"protocol_version", initialized.ProtocolVersion,
		"backend", initialized.Backend,
		"persistent", initialized.Persistent,
		"borderless_access", initialized.BorderlessAccess,
		"border_required", initialized.BorderRequired,
		"monitor", initialized.Status.Monitor.DeviceName,
	)
	return client, nil
}

func (c *processClient) PID() int { return c.pid }

func (c *processClient) Status(ctx context.Context, deadline time.Time) (capture.Status, error) {
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result capture.Status
	if err := c.call(callCtx, methodStatus, deadlineParams{Deadline: deadline}, &result); err != nil {
		return capture.Status{}, err
	}
	return result, nil
}

func (c *processClient) Capture(ctx context.Context, deadline time.Time, request capture.Request) (capture.Result, error) {
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result captureResult
	if err := c.call(callCtx, methodCapture, captureParams{Deadline: deadline, Request: request}, &result); err != nil {
		return capture.Result{}, err
	}
	return result.Result, nil
}

func (c *processClient) CaptureRegion(ctx context.Context, deadline time.Time, request capture.RegionRequest) (capture.RegionResult, error) {
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result regionResult
	if err := c.call(callCtx, methodCaptureRegion, regionParams{Deadline: deadline, Request: request}, &result); err != nil {
		return capture.RegionResult{}, err
	}
	return result.Result, nil
}

func (c *processClient) call(ctx context.Context, method string, params, target any) error {
	if ctx == nil {
		return errors.New("WGC worker call context is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("WGC worker process is closed")
	}
	c.nextID++
	id := fmt.Sprintf("wgc-%d", c.nextID)
	encoded, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode WGC worker request: %w", err)
	}
	result := make(chan error, 1)
	go func() {
		if err := c.conn.Write(observationprotocol.Message{ID: id, Method: method, Params: encoded}); err != nil {
			result <- err
			return
		}
		response, err := c.conn.Read()
		if err != nil {
			result <- fmt.Errorf("read response from WGC worker process %d: %w", c.pid, err)
			return
		}
		if response.ID != id || response.Method != "" {
			result <- errors.New("WGC worker response ID or shape mismatch")
			return
		}
		if response.Error != nil {
			var data errorData
			if len(response.Error.Data) != 0 {
				if err := decodeStrict(response.Error.Data, &data); err != nil {
					result <- fmt.Errorf("decode WGC worker error data: %w", err)
					return
				}
			}
			cause := errors.New(data.Cause)
			if data.Code != "" {
				result <- capture.Failure(data.Code, response.Error.Message, cause)
				return
			}
			result <- fmt.Errorf("WGC worker %d: %s: %w", response.Error.Code, response.Error.Message, cause)
			return
		}
		if err := decodeStrict(response.Result, target); err != nil {
			result <- fmt.Errorf("decode WGC worker result: %w", err)
			return
		}
		result <- nil
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = c.command.Process.Kill()
		select {
		case <-result:
		case <-time.After(3 * time.Second):
		}
		return ctx.Err()
	}
}

func (c *processClient) Close() error {
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var stopped shutdownResult
	err := c.call(ctx, methodShutdown, deadlineParams{Deadline: time.Now().Add(2 * time.Second)}, &stopped)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	waitErr := c.command.Wait()
	if err != nil {
		if c.command.ProcessState == nil {
			_ = c.command.Process.Kill()
			_ = c.command.Wait()
		}
		return fmt.Errorf("shutdown WGC worker: %w: %s", err, c.stderr.String())
	}
	if stopped.State != "stopped" {
		return errors.New("WGC worker shutdown response is invalid")
	}
	if waitErr != nil {
		return fmt.Errorf("wait for WGC worker shutdown: %w: %s", waitErr, c.stderr.String())
	}
	return nil
}

type boundedBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, data...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(data), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.data))
}
