//go:build windows && amd64

package wgc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	winapirt "github.com/whiteboxsolutions/winapi/winrt"
)

type persistentRequestKind uint8

const (
	persistentFull persistentRequestKind = iota + 1
	persistentRegion
)

type persistentRequest struct {
	ctx         context.Context
	kind        persistentRequestKind
	full        capture.Request
	region      capture.RegionRequest
	response    chan persistentResponse
	operationID uint64
	trace       bool
}

type persistentResponse struct {
	full   capture.Result
	region capture.RegionResult
	err    error
}

type persistentInitialization struct {
	status PersistentStatus
	err    error
}

// PersistentStatus is the initialization contract for the resident WGC runtime.
// BorderlessAccess is "allowed" only after Windows grants the borderless
// capability and IsBorderRequired has been set and read back as false.
type PersistentStatus struct {
	Capture          capture.Status
	BorderlessAccess string
	BorderRequired   bool
}

// PersistentCapturer keeps the WGC item, D3D11 device/context, and region
// shader resident. Each serial request creates its own frame pool and capture
// session so a static desktop still produces a frame after request acceptance.
type PersistentCapturer struct {
	logger   *slog.Logger
	requests chan persistentRequest
	cancel   context.CancelFunc
	done     chan struct{}
	status   PersistentStatus
	close    sync.Once
	errMu    sync.Mutex
	runErr   error
	trace    atomic.Bool
	sequence atomic.Uint64
}

func NewPersistent(initializationContext context.Context, logger *slog.Logger) (*PersistentCapturer, error) {
	if initializationContext == nil {
		return nil, errors.New("persistent WGC initialization context is required")
	}
	if logger == nil {
		return nil, errors.New("persistent WGC logger is required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan persistentRequest)
	initialized := make(chan persistentInitialization, 1)
	done := make(chan struct{})
	capturer := &PersistentCapturer{logger: logger, requests: requests, cancel: cancel, done: done}
	go func() {
		capturer.errMu.Lock()
		capturer.runErr = runPersistentSession(ctx, initializationContext, logger, requests, initialized)
		capturer.errMu.Unlock()
		close(done)
	}()
	result := <-initialized
	if result.err != nil {
		cancel()
		<-done
		return nil, result.err
	}
	capturer.status = result.status
	return capturer, nil
}

func (c *PersistentCapturer) Status(context.Context) (capture.Status, error) {
	if c == nil {
		return capture.Status{}, errors.New("persistent WGC capturer is required")
	}
	return c.status.Capture, nil
}

func (c *PersistentCapturer) PersistentStatus() (PersistentStatus, error) {
	if c == nil {
		return PersistentStatus{}, errors.New("persistent WGC capturer is required")
	}
	return c.status, nil
}

func (c *PersistentCapturer) SetTrace(enabled bool) {
	if c != nil {
		c.trace.Store(enabled)
	}
}

func (c *PersistentCapturer) Capture(ctx context.Context, request capture.Request) (capture.Result, error) {
	profile, err := capture.ParseProfile(string(request.Profile))
	if err != nil {
		return capture.Result{}, err
	}
	request.Profile = profile
	response, err := c.request(ctx, persistentRequest{ctx: ctx, kind: persistentFull, full: request})
	if err != nil {
		return capture.Result{}, err
	}
	return response.full, response.err
}

func (c *PersistentCapturer) CaptureRegion(ctx context.Context, request capture.RegionRequest) (capture.RegionResult, error) {
	if err := request.Region.Validate(); err != nil {
		return capture.RegionResult{}, err
	}
	if request.Sampling != capture.SamplingReference && request.Sampling != capture.SamplingNative {
		return capture.RegionResult{}, errors.New("region sampling must equal reference or native")
	}
	if request.MaxPixels == 0 || request.MaxPixels > 262_144 {
		return capture.RegionResult{}, errors.New("region maxPixels must be from 1 through 262144")
	}
	response, err := c.request(ctx, persistentRequest{ctx: ctx, kind: persistentRegion, region: request})
	if err != nil {
		return capture.RegionResult{}, err
	}
	return response.region, response.err
}

func (c *PersistentCapturer) request(ctx context.Context, request persistentRequest) (persistentResponse, error) {
	if c == nil {
		return persistentResponse{}, errors.New("persistent WGC capturer is required")
	}
	if ctx == nil {
		return persistentResponse{}, errors.New("persistent WGC request context is required")
	}
	request.response = make(chan persistentResponse, 1)
	operationID := c.sequence.Add(1)
	request.operationID = operationID
	request.trace = c.trace.Load()
	started := time.Now()
	if request.trace {
		c.logger.Info("persistent_wgc_request_started", "operation_id", operationID, "capture_kind", request.kind.String())
	}
	select {
	case c.requests <- request:
	case <-ctx.Done():
		return persistentResponse{}, ctx.Err()
	case <-c.done:
		return persistentResponse{}, fmt.Errorf("persistent WGC session stopped: %w", c.sessionError())
	}
	select {
	case response := <-request.response:
		if request.trace {
			attributes := []any{"operation_id", operationID, "capture_kind", request.kind.String(), "duration_ms", time.Since(started).Milliseconds()}
			if response.err != nil {
				attributes = append(attributes, "error", response.err)
				c.logger.Error("persistent_wgc_request_failed", attributes...)
			} else {
				c.logger.Info("persistent_wgc_request_completed", attributes...)
			}
		}
		return response, nil
	case <-ctx.Done():
		return persistentResponse{}, ctx.Err()
	case <-c.done:
		return persistentResponse{}, fmt.Errorf("persistent WGC session stopped: %w", c.sessionError())
	}
}

func (k persistentRequestKind) String() string {
	switch k {
	case persistentFull:
		return "full"
	case persistentRegion:
		return "region"
	default:
		return "unknown"
	}
}

func (c *PersistentCapturer) Close() error {
	if c == nil {
		return nil
	}
	c.close.Do(c.cancel)
	<-c.done
	err := c.sessionError()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (c *PersistentCapturer) sessionError() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.runErr == nil {
		return errors.New("persistent WGC session exited without an error")
	}
	return c.runErr
}

func runPersistentSession(
	ctx context.Context,
	initializationContext context.Context,
	logger *slog.Logger,
	requests <-chan persistentRequest,
	initialized chan<- persistentInitialization,
) (returnErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPI, err := enterPerMonitorV2DPIAwareness()
	if err != nil {
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer restoreDPI()
	hr, _, _ := procRoInitialize.Call(uintptr(winapirt.RO_INIT_MULTITHREADED))
	if hr != 0 && hr != 1 {
		err = fmt.Errorf("RoInitialize(MTA): HRESULT %#08x", uint32(hr))
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer procRoUninitialize.Call()

	supported, err := graphicsCaptureSupported()
	if err != nil {
		err = capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	if !supported {
		err = capture.Failure("capture_unsupported", "Windows Graphics Capture is not supported", nil)
		initialized <- persistentInitialization{err: err}
		return err
	}
	target, err := findPrimaryDisplay()
	if err != nil {
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer release(target.adapter)
	monitor := monitorFromDesc(target.desc)
	pixelFormat := uint32(dxgiFormatB8G8R8A8UNorm)
	pixelFormatName := "B8G8R8A8_UNORM"
	switch target.desc.ColorSpace {
	case dxgiColorSpaceRGBFullG22NoneP709:
	case dxgiColorSpaceRGBFullG2084NoneP2020:
		if !finitePositiveAbove80(float64(target.desc.MaxLuminance)) {
			err = capture.Failure("invalid_hdr_metadata", "HDR display metadata must provide finite maximum luminance above 80 nits", nil)
			initialized <- persistentInitialization{err: err}
			return err
		}
		pixelFormat = dxgiFormatR16G16B16A16Float
		pixelFormatName = "R16G16B16A16_FLOAT"
	default:
		err = capture.Failure("unsupported_color_space", fmt.Sprintf("unsupported primary-monitor color space: %s", colorSpaceName(target.desc.ColorSpace)), nil)
		initialized <- persistentInitialization{err: err}
		return err
	}
	device, context3D, winRTDevice, err := createD3DDevice(target.adapter)
	if err != nil {
		err = capture.Failure("capture_device_failed", "failed to create the persistent Direct3D 11 device", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer release(device)
	defer release(context3D)
	defer release(winRTDevice)
	item, size, err := createMonitorItem(target.desc.Monitor)
	if err != nil {
		err = capture.Failure("desktop_unavailable", "failed to create the persistent capture item", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer release(item)
	if size.Width <= 0 || size.Height <= 0 {
		err = capture.Failure("desktop_unavailable", "primary monitor capture size is invalid", nil)
		initialized <- persistentInitialization{err: err}
		return err
	}
	framePool, err := createFreeThreadedFramePoolWithBuffers(winRTDevice, int32(pixelFormat), size, 2)
	if err != nil {
		err = capture.Failure("capture_session_failed", "failed to create the persistent frame pool", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer func() { closeAndRelease(framePool) }()
	session, err := createCaptureSession(framePool, item)
	if err != nil {
		err = capture.Failure("capture_session_failed", "failed to create the persistent capture session", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer func() { closeAndRelease(session) }()
	if err = setCursorCapture(session, false); err != nil {
		err = capture.Failure("capture_session_failed", "failed to disable cursor capture", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	if err = requestBorderlessCapture(initializationContext); err != nil {
		err = capture.Failure("capture_borderless_access_failed", "failed to obtain borderless persistent WGC capture", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	if err = setBorderRequired(session, false); err != nil {
		err = capture.Failure("capture_session_failed", "failed to disable the persistent WGC capture border", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	if err = callHRESULT(session, 6); err != nil {
		err = capture.Failure("capture_session_failed", "failed to start persistent WGC capture", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	shader, err := createRegionComputeShader(device)
	if err != nil {
		err = capture.Failure("capture_region_shader_failed", "failed to create the persistent region shader", err)
		initialized <- persistentInitialization{err: err}
		return err
	}
	defer release(shader)
	status := PersistentStatus{
		Capture:          capture.Status{Supported: true, Monitor: monitor},
		BorderlessAccess: "allowed",
		BorderRequired:   false,
	}
	// Initialization proves borderless access before the worker becomes ready.
	// Keeping this frame pool would let a static desktop contribute an old frame
	// to a later request, so requests start with their own empty pool.
	closeAndRelease(session)
	session = nil
	closeAndRelease(framePool)
	framePool = nil
	initialized <- persistentInitialization{status: status}
	logger.Info("persistent_wgc_runtime_started", "width", size.Width, "height", size.Height, "pixel_format", pixelFormatName,
		"borderless_access", status.BorderlessAccess, "border_required", status.BorderRequired)
	defer logger.Info("persistent_wgc_runtime_stopped")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request := <-requests:
			if err := request.ctx.Err(); err != nil {
				request.response <- persistentResponse{err: err}
				continue
			}
			response := captureFreshRequest(request, logger, winRTDevice, item, size, pixelFormat,
				device, context3D, pixelFormatName, target.desc, monitor, shader)
			request.response <- response
		}
	}
}

func captureFreshRequest(request persistentRequest, logger *slog.Logger, winRTDevice, item unsafe.Pointer,
	size winapirt.SizeInt32, pixelFormat uint32, device, context3D unsafe.Pointer, pixelFormatName string,
	display outputDesc1, monitor capture.Monitor, shader unsafe.Pointer,
) (response persistentResponse) {
	started := time.Now()
	stage := "create_frame_pool"
	defer func() {
		if response.err != nil {
			logger.Error("persistent_wgc_capture_failed", "operation_id", request.operationID,
				"stage", stage, "duration_ms", time.Since(started).Milliseconds(), "error", response.err)
		}
	}()
	framePool, err := createFreeThreadedFramePoolWithBuffers(winRTDevice, int32(pixelFormat), size, 2)
	if err != nil {
		response.err = capture.Failure("capture_session_failed", "failed to create request WGC frame pool", err)
		return
	}
	defer closeAndRelease(framePool)
	stage = "create_session"
	session, err := createCaptureSession(framePool, item)
	if err != nil {
		response.err = capture.Failure("capture_session_failed", "failed to create request WGC session", err)
		return
	}
	defer closeAndRelease(session)
	stage = "configure_session"
	requestedCursor := request.kind == persistentFull && request.full.IncludeCursor
	if err = setCursorCapture(session, requestedCursor); err != nil {
		response.err = capture.Failure("capture_session_failed", "failed to set request cursor capture", err)
		return
	}
	if err = setBorderRequired(session, false); err != nil {
		response.err = capture.Failure("capture_session_failed", "failed to verify request borderless capture", err)
		return
	}
	stage = "start_capture"
	if err = callHRESULT(session, 6); err != nil {
		response.err = capture.Failure("capture_session_failed", "failed to start request WGC capture", err)
		return
	}
	stage = "wait_for_frame"
	if request.trace {
		logger.Info("persistent_wgc_frame_wait_started", "operation_id", request.operationID,
			"elapsed_ms", time.Since(started).Milliseconds())
	}
	frame, err := latestFrame(request.ctx, framePool)
	if err != nil {
		response.err = err
		return
	}
	defer closeAndRelease(frame)
	if request.trace {
		logger.Info("persistent_wgc_frame_acquired", "operation_id", request.operationID,
			"elapsed_ms", time.Since(started).Milliseconds())
	}
	stage = "foreground"
	foregroundInfo, err := foreground.Snapshot()
	if err != nil {
		response.err = capture.Failure("foreground_process_unavailable", "failed to identify the foreground process at capture time", err)
		return
	}
	captured := capturedFrame{frame: frame, device: device, context3D: context3D,
		pixelFormat: pixelFormat, pixelFormatName: pixelFormatName,
		display: display, monitor: monitor, foreground: foregroundInfo,
		width: int(size.Width), height: int(size.Height)}
	stage = "process_frame"
	switch request.kind {
	case persistentFull:
		response.full, response.err = captureFullFrame(captured, request.full)
	case persistentRegion:
		response.region, response.err = captureRegionFrame(captured, request.region, shader)
	default:
		response.err = errors.New("unknown persistent WGC request kind")
	}
	if request.trace && response.err == nil {
		logger.Info("persistent_wgc_capture_completed", "operation_id", request.operationID,
			"duration_ms", time.Since(started).Milliseconds(), "capture_kind", request.kind.String())
	}
	return
}
