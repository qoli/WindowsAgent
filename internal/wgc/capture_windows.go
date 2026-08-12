//go:build windows && amd64

package wgc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/qoli/WindowsAgent/internal/capture"
	"github.com/qoli/WindowsAgent/internal/foreground"
	"github.com/qoli/WindowsAgent/internal/pixels"
	"github.com/qoli/WindowsAgent/internal/videocapture"
	"github.com/whiteboxsolutions/go-ole"
	winapirt "github.com/whiteboxsolutions/winapi/winrt"
	"golang.org/x/sys/windows"
)

const (
	dxgiErrorNotFound = 0x887a0002

	dxgiColorSpaceRGBFullG22NoneP709    = 0
	dxgiColorSpaceRGBFullG10NoneP709    = 1
	dxgiColorSpaceRGBFullG2084NoneP2020 = 12

	d3dDriverTypeUnknown         = 0
	d3d11CreateDeviceBGRASupport = 0x20
	d3d11SDKVersion              = 7
	d3d11UsageStaging            = 3
	d3d11UsageDefault            = 0
	d3d11CPUAccessRead           = 0x20000
	d3d11BindConstantBuffer      = 0x4
	d3d11BindShaderResource      = 0x8
	d3d11BindUnorderedAccess     = 0x80

	dxgiFormatR16G16B16A16Float = 10
	dxgiFormatR32Uint           = 42
	dxgiFormatB8G8R8A8UNorm     = 87

	monitorDefaultToPrimary = 2
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modCombase  = windows.NewLazySystemDLL("combase.dll")
	modDXGI     = windows.NewLazySystemDLL("dxgi.dll")
	modD3D11    = windows.NewLazySystemDLL("d3d11.dll")
	modCompiler = windows.NewLazySystemDLL("d3dcompiler_47.dll")

	procSetProcessDPIAwarenessContext        = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procMonitorFromPoint                     = modUser32.NewProc("MonitorFromPoint")
	procRoInitialize                         = modCombase.NewProc("RoInitialize")
	procRoUninitialize                       = modCombase.NewProc("RoUninitialize")
	procCreateDXGIFactory1                   = modDXGI.NewProc("CreateDXGIFactory1")
	procD3D11CreateDevice                    = modD3D11.NewProc("D3D11CreateDevice")
	procD3DCompile                           = modCompiler.NewProc("D3DCompile")
	procCreateDirect3D11DeviceFromDXGIDevice = modD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")

	iidIDXGIFactory1 = ole.NewGUID("{770aae78-f26f-4dba-a829-253c83d1b387}")
	iidIDXGIOutput6  = ole.NewGUID("{068346e8-aaec-4b84-add7-137f513f77a1}")
	iidIDXGIDevice   = ole.NewGUID("{54ec77fa-1377-44e6-8c32-88fd5f44c84c}")
	iidTexture2D     = ole.NewGUID("{6f15aaf2-d208-4e89-9ab4-489535d34f9c}")
	iidDXGIInterface = ole.NewGUID("{a9b3d012-3df2-4ee3-b8d1-8695f457d3c1}")
	iidClosable      = ole.NewGUID("{30d5a829-7fa4-4026-83bb-d75bae4ea99e}")
)

type Capturer struct {
	logger      *slog.Logger
	captureGate chan struct{}
	sequence    atomic.Uint64
	active      atomic.Int64
	trace       atomic.Bool
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type outputDesc1 struct {
	DeviceName            [32]uint16
	DesktopCoordinates    rect
	AttachedToDesktop     int32
	Rotation              uint32
	Monitor               uintptr
	BitsPerColor          uint32
	ColorSpace            int32
	RedPrimary            [2]float32
	GreenPrimary          [2]float32
	BluePrimary           [2]float32
	WhitePoint            [2]float32
	MinLuminance          float32
	MaxLuminance          float32
	MaxFullFrameLuminance float32
}

type displayTarget struct {
	adapter unsafe.Pointer
	desc    outputDesc1
}

type sampleDesc struct {
	Count   uint32
	Quality uint32
}

type texture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     sampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type mappedSubresource struct {
	Data       unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

type d3d11Box struct {
	Left, Top, Front, Right, Bottom, Back uint32
}

type bufferDesc struct {
	ByteWidth           uint32
	Usage               uint32
	BindFlags           uint32
	CPUAccessFlags      uint32
	MiscFlags           uint32
	StructureByteStride uint32
}

type subresourceData struct {
	SystemMemory           unsafe.Pointer
	SystemMemoryPitch      uint32
	SystemMemorySlicePitch uint32
}

type regionShaderConstants struct {
	SourceWidth  uint32
	SourceHeight uint32
	OutputWidth  uint32
	OutputHeight uint32
	WhiteSquared float32
	HDR          uint32
	Padding      [2]uint32
}

const regionShaderSource = `
cbuffer RegionConstants : register(b0) {
    uint2 sourceSize;
    uint2 outputSize;
    float whiteSquared;
    uint sourceIsHDR;
    uint2 padding;
};

Texture2D<float4> sourceTexture : register(t0);
RWTexture2D<uint> outputTexture : register(u0);

float encodeSRGB(float value) {
    value = saturate(value);
    if (value <= 0.0031308) {
        return 12.92 * value;
    }
    return 1.055 * pow(value, 1.0 / 2.4) - 0.055;
}

[numthreads(8, 8, 1)]
void main(uint3 dispatchID : SV_DispatchThreadID) {
    if (dispatchID.x >= outputSize.x || dispatchID.y >= outputSize.y) {
        return;
    }

    float2 sourcePosition =
        (float2(dispatchID.xy) + 0.5) * float2(sourceSize) / float2(outputSize) - 0.5;
    int2 lower = clamp(int2(floor(sourcePosition)), int2(0, 0), int2(sourceSize) - 1);
    int2 upper = min(lower + 1, int2(sourceSize) - 1);
    float2 fraction = saturate(sourcePosition - float2(lower));

    float4 top = lerp(sourceTexture.Load(int3(lower.x, lower.y, 0)),
                      sourceTexture.Load(int3(upper.x, lower.y, 0)), fraction.x);
    float4 bottom = lerp(sourceTexture.Load(int3(lower.x, upper.y, 0)),
                         sourceTexture.Load(int3(upper.x, upper.y, 0)), fraction.x);
    float3 rgb = max(lerp(top, bottom, fraction.y).rgb, 0.0);

    if (sourceIsHDR != 0) {
        float luminance = dot(rgb, float3(0.2126, 0.7152, 0.0722));
        if (luminance > 0.0) {
            float mapped = luminance * (1.0 + luminance / whiteSquared) / (1.0 + luminance);
            rgb *= mapped / luminance;
        }
        rgb = float3(encodeSRGB(rgb.r), encodeSRGB(rgb.g), encodeSRGB(rgb.b));
    }

    uint3 encoded = uint3(round(saturate(rgb) * 255.0));
    outputTexture[dispatchID.xy] = (encoded.r << 16) | (encoded.g << 8) | encoded.b;
}
`

func New(logger *slog.Logger) (*Capturer, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	dpiContextPerMonitorAwareV2 := ^uintptr(3)
	ok, _, callErr := procSetProcessDPIAwarenessContext.Call(dpiContextPerMonitorAwareV2)
	if ok == 0 {
		return nil, fmt.Errorf("set per-monitor-v2 DPI awareness: %w", callErr)
	}
	// WGC and the D3D11 immediate context are process-global native pressure
	// points even though each request owns its COM objects. Keep only one
	// capture/readback operation in native code at a time. Live evidence showed
	// that overlapping Compass and OCR region captures could terminate the
	// whole Agent inside ID3D11DeviceContext::Map with 0xc0000005.
	return &Capturer{logger: logger, captureGate: make(chan struct{}, 1)}, nil
}

func (c *Capturer) SetTrace(enabled bool) {
	c.trace.Store(enabled)
}

func (c *Capturer) Status(ctx context.Context) (capture.Status, error) {
	return onWinRTThread(ctx, func() (capture.Status, error) {
		supported, err := graphicsCaptureSupported()
		if err != nil {
			return capture.Status{}, capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		}
		if !supported {
			return capture.Status{Supported: false}, nil
		}
		target, err := findPrimaryDisplay()
		if err != nil {
			return capture.Status{}, err
		}
		defer release(target.adapter)
		return capture.Status{
			Supported: true,
			Monitor:   monitorFromDesc(target.desc),
		}, nil
	})
}

func (c *Capturer) Capture(ctx context.Context, request capture.Request) (capture.Result, error) {
	profile, err := capture.ParseProfile(string(request.Profile))
	if err != nil {
		return capture.Result{}, err
	}
	request.Profile = profile
	var result capture.Result
	err = c.withCapturedFrame(ctx, "full:"+string(profile), request.IncludeCursor, func(frame capturedFrame) error {
		processedAt := time.Now()
		frameImage, readWidth, readHeight, readErr := readFrame(frame.frame, frame.device, frame.context3D, frame.pixelFormat, frame.display)
		if readErr != nil {
			return readErr
		}
		if readWidth != frame.width || readHeight != frame.height {
			return capture.Failure(
				"capture_size_mismatch",
				fmt.Sprintf("captured texture is %dx%d but WGC item is %dx%d", readWidth, readHeight, frame.width, frame.height),
				nil,
			)
		}
		var content []byte
		width, height := frame.width, frame.height
		format, contentType, extension := "jpeg", "image/jpeg", ".jpg"
		quality, chroma := 90, "444"
		switch profile {
		case capture.ProfileNativeJPEG, capture.Profile1080pJPEG:
			if profile == capture.Profile1080pJPEG {
				width, height = fitInside(frame.width, frame.height, 1920, 1080)
			}
			bgr, convertErr := pixels.NRGBAToBGR(frameImage, width, height)
			if convertErr != nil {
				return capture.Failure("capture_encode_failed", "failed to prepare the captured frame for JPEG encoding", convertErr)
			}
			var encodeErr error
			content, encodeErr = encodeWICJPEG(bgr, width, height, quality)
			if encodeErr != nil {
				return capture.Failure("capture_encode_failed", "failed to encode captured frame as JPEG Q90 4:4:4", encodeErr)
			}
		case capture.ProfileNativePNG:
			var encoded bytes.Buffer
			encoder := png.Encoder{CompressionLevel: png.BestSpeed}
			if encodeErr := encoder.Encode(&encoded, frameImage); encodeErr != nil {
				return capture.Failure("capture_encode_failed", "failed to encode captured frame as PNG", encodeErr)
			}
			content = encoded.Bytes()
			format, contentType, extension = "png", "image/png", ".png"
			quality, chroma = 0, ""
		default:
			return fmt.Errorf("unsupported capture profile %q", profile)
		}
		monitor := frame.monitor
		monitor.Width = frame.width
		monitor.Height = frame.height
		result = capture.Result{
			Content: content, Profile: profile, Format: format, ContentType: contentType,
			FileExtension: extension, Quality: quality, ChromaSubsampling: chroma,
			Width: width, Height: height,
			IncludeCursor: request.IncludeCursor, Monitor: monitor, Foreground: frame.foreground,
			CapturePixelFormat: frame.pixelFormatName, ToneMapped: monitor.HDR,
		}
		c.logCaptureLifecycle(ctx, "wgc_capture_processed",
			"profile", profile, "width", width, "height", height,
			"bytes", len(content), "process_ms", time.Since(processedAt).Milliseconds(),
		)
		return nil
	})
	return result, err
}

// Run owns one persistent WGC session and samples its newest available frame
// at each whole UTC interval. It never enters the request-driven capture path.
func (c *Capturer) Run(ctx context.Context, interval time.Duration, lifecycle videocapture.Lifecycle, consume videocapture.Consumer) error {
	if ctx == nil || lifecycle == nil || consume == nil {
		return errors.New("video stream context, lifecycle, and consumer are required")
	}
	if interval != time.Second {
		return errors.New("WGC evidence stream interval must equal one second")
	}
	select {
	case c.captureGate <- struct{}{}:
		defer func() { <-c.captureGate }()
	case <-ctx.Done():
		return nil
	}
	_, err := onWinRTThread(ctx, func() (struct{}, error) {
		supported, err := graphicsCaptureSupported()
		if err != nil {
			return struct{}{}, capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		}
		if !supported {
			return struct{}{}, capture.Failure("capture_unsupported", "Windows Graphics Capture is not supported", nil)
		}
		target, err := findPrimaryDisplay()
		if err != nil {
			return struct{}{}, err
		}
		defer release(target.adapter)
		monitor := monitorFromDesc(target.desc)
		pixelFormat := uint32(dxgiFormatB8G8R8A8UNorm)
		pixelFormatName := "B8G8R8A8_UNORM"
		switch target.desc.ColorSpace {
		case dxgiColorSpaceRGBFullG22NoneP709:
		case dxgiColorSpaceRGBFullG2084NoneP2020:
			if !finitePositiveAbove80(float64(target.desc.MaxLuminance)) {
				return struct{}{}, capture.Failure("invalid_hdr_metadata", "HDR display metadata must provide finite maximum luminance above 80 nits", nil)
			}
			pixelFormat = dxgiFormatR16G16B16A16Float
			pixelFormatName = "R16G16B16A16_FLOAT"
		default:
			return struct{}{}, capture.Failure("unsupported_color_space", fmt.Sprintf("unsupported primary-monitor color space: %s", colorSpaceName(target.desc.ColorSpace)), nil)
		}
		device, context3D, winRTDevice, err := createD3DDevice(target.adapter)
		if err != nil {
			return struct{}{}, capture.Failure("capture_device_failed", "failed to create the Direct3D 11 video-stream device", err)
		}
		defer release(device)
		defer release(context3D)
		defer release(winRTDevice)
		item, size, err := createMonitorItem(target.desc.Monitor)
		if err != nil {
			return struct{}{}, capture.Failure("desktop_unavailable", "failed to create the persistent capture item", err)
		}
		defer release(item)
		if size.Width < 1920 || size.Height < 1080 || int64(size.Width)*1080 != int64(size.Height)*1920 {
			return struct{}{}, capture.Failure("capture_size_unsupported", fmt.Sprintf("persistent evidence capture requires a 16:9 display at least 1920x1080, got %dx%d", size.Width, size.Height), nil)
		}
		framePool, err := createFreeThreadedFramePoolWithBuffers(winRTDevice, int32(pixelFormat), size, 2)
		if err != nil {
			return struct{}{}, capture.Failure("capture_session_failed", "failed to create the persistent WGC frame pool", err)
		}
		defer closeAndRelease(framePool)
		session, err := createCaptureSession(framePool, item)
		if err != nil {
			return struct{}{}, capture.Failure("capture_session_failed", "failed to create the persistent WGC session", err)
		}
		defer closeAndRelease(session)
		if err = setCursorCapture(session, false); err != nil {
			return struct{}{}, capture.Failure("capture_session_failed", "failed to disable cursor capture for persistent evidence", err)
		}
		if err = requestBorderlessCapture(ctx); err != nil {
			return struct{}{}, capture.Failure("capture_borderless_access_failed", "failed to obtain borderless persistent evidence capture", err)
		}
		if err = setBorderRequired(session, false); err != nil {
			return struct{}{}, capture.Failure("capture_session_failed", "failed to disable the persistent evidence capture border", err)
		}
		if err = callHRESULT(session, 6); err != nil {
			return struct{}{}, capture.Failure("capture_session_failed", "failed to start persistent WGC capture", err)
		}
		if err = lifecycle.Start(); err != nil {
			return struct{}{}, capture.Failure("capture_lifecycle_failed", "failed to publish the persistent evidence recording state", err)
		}
		defer lifecycle.Stop()
		videoShader, err := createRegionComputeShader(device)
		if err != nil {
			return struct{}{}, capture.Failure("capture_region_shader_failed", "failed to create the persistent video sampling shader", err)
		}
		defer release(videoShader)
		c.logCaptureLifecycle(ctx, "wgc_video_stream_started", "width", size.Width, "height", size.Height, "interval_ms", interval.Milliseconds())
		defer c.logCaptureLifecycle(ctx, "wgc_video_stream_stopped")

		next := time.Now().UTC().Truncate(interval).Add(interval)
		sequence := uint64(0)
		for {
			if err = waitUntil(ctx, next); err != nil {
				return struct{}{}, nil
			}
			slotContext, cancel := context.WithDeadline(ctx, next.Add(interval))
			frame, frameErr := latestFrame(slotContext, framePool)
			cancel()
			if ctx.Err() != nil {
				if frame != nil {
					closeAndRelease(frame)
				}
				return struct{}{}, nil
			}
			if frameErr != nil {
				if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Stage: "wgc_frame", Err: frameErr}); err != nil {
					return struct{}{}, err
				}
			} else {
				captured := capturedFrame{frame: frame, device: device, context3D: context3D, pixelFormat: pixelFormat, pixelFormatName: pixelFormatName, display: target.desc, monitor: monitor, width: int(size.Width), height: int(size.Height)}
				region := capture.PixelRegion{Left: 0, Top: 0, Width: int(size.Width), Height: int(size.Height)}
				videoPixels, readErr := readRegionPixelsWithShader(captured, region, 1920, 1080, videoShader)
				closeAndRelease(frame)
				if readErr != nil {
					if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Stage: "wgc_readback", Err: readErr}); err != nil {
						return struct{}{}, err
					}
				} else {
					foregroundInfo, foregroundErr := foreground.Snapshot()
					if foregroundErr != nil {
						if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Stage: "foreground", Err: foregroundErr}); err != nil {
							return struct{}{}, err
						}
					} else {
						bgrx, convertErr := pixels.RGB24WordsToBGRXBottomUp(videoPixels, 1920, 1080)
						if convertErr != nil {
							if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Stage: "pixel_conversion", Err: convertErr}); err != nil {
								return struct{}{}, err
							}
						} else {
							sequence++
							videoFrame := videocapture.Frame{Sequence: sequence, ScheduledAt: next, ObservedAt: foregroundInfo.ObservedAt.UTC(), ForegroundExecutable: foregroundInfo.ExecutableName, Width: 1920, Height: 1080, PixelFormat: videocapture.PixelFormatBGRX32BottomUp, Pixels: bgrx}
							if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Frame: &videoFrame}); err != nil {
								return struct{}{}, err
							}
						}
					}
				}
			}
			next = next.Add(interval)
			for !next.Add(interval).After(time.Now().UTC()) {
				overrun := errors.New("persistent video processing did not finish before this one-second slot")
				if err = consume(ctx, videocapture.Sample{ScheduledAt: next, Stage: "scheduler_overrun", Err: overrun}); err != nil {
					return struct{}{}, err
				}
				next = next.Add(interval)
			}
		}
	})
	return err
}

func waitUntil(ctx context.Context, at time.Time) error {
	delay := time.Until(at)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func latestFrame(ctx context.Context, framePool unsafe.Pointer) (unsafe.Pointer, error) {
	frame, err := waitForFrame(ctx, framePool)
	if err != nil {
		return nil, err
	}
	for {
		var newer unsafe.Pointer
		if err = callHRESULTWith(framePool, 7, uintptr(unsafe.Pointer(&newer))); err != nil {
			closeAndRelease(frame)
			return nil, capture.Failure("capture_frame_failed", "WGC failed while draining the persistent frame pool", err)
		}
		if newer == nil {
			return frame, nil
		}
		closeAndRelease(frame)
		frame = newer
	}
}

func fitInside(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	scale := math.Min(float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

func (c *Capturer) CaptureRegion(ctx context.Context, request capture.RegionRequest) (capture.RegionResult, error) {
	if err := request.Region.Validate(); err != nil {
		return capture.RegionResult{}, err
	}
	if request.Sampling != capture.SamplingReference && request.Sampling != capture.SamplingNative {
		return capture.RegionResult{}, errors.New("region sampling must equal reference or native")
	}
	if request.MaxPixels == 0 || request.MaxPixels > 262_144 {
		return capture.RegionResult{}, errors.New("region maxPixels must be from 1 through 262144")
	}

	var result capture.RegionResult
	err := c.withCapturedFrame(ctx, "region", false, func(frame capturedFrame) error {
		viewport, physical, err := capture.MapReferenceRegion(frame.width, frame.height, request.Region)
		if err != nil {
			return capture.Failure("screen_region_invalid", "failed to map the reference screen region", err)
		}
		outputWidth, outputHeight := physical.Width, physical.Height
		if request.Sampling == capture.SamplingReference {
			outputWidth, outputHeight = request.Region.Width, request.Region.Height
		}
		pixelCount := uint64(outputWidth) * uint64(outputHeight)
		if pixelCount > request.MaxPixels {
			return capture.Failure(
				"screen_pixel_limit_exceeded",
				fmt.Sprintf("mapped screen region returns %d pixels, limit is %d", pixelCount, request.MaxPixels),
				nil,
			)
		}
		pixels, err := readRegionPixels(frame, physical, outputWidth, outputHeight)
		if err != nil {
			return err
		}
		result = capture.RegionResult{
			Pixels: pixels, ImageWidth: outputWidth, ImageHeight: outputHeight,
			FrameWidth: frame.width, FrameHeight: frame.height,
			Viewport: viewport, PhysicalRegion: physical,
			Monitor: frame.monitor, Foreground: frame.foreground,
			CapturePixelFormat: frame.pixelFormatName, ToneMapped: frame.monitor.HDR,
		}
		return nil
	})
	return result, err
}

type capturedFrame struct {
	frame           unsafe.Pointer
	device          unsafe.Pointer
	context3D       unsafe.Pointer
	pixelFormat     uint32
	pixelFormatName string
	display         outputDesc1
	monitor         capture.Monitor
	foreground      foreground.Info
	width           int
	height          int
}

func (c *Capturer) withCapturedFrame(
	ctx context.Context,
	captureKind string,
	includeCursor bool,
	process func(capturedFrame) error,
) (returnErr error) {
	if process == nil {
		return errors.New("captured frame processor is required")
	}
	queuedAt := time.Now()
	select {
	case c.captureGate <- struct{}{}:
		defer func() { <-c.captureGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	queueWait := time.Since(queuedAt)
	if queueWait >= time.Millisecond {
		c.logCaptureLifecycle(
			ctx,
			"wgc_capture_dequeued",
			"capture_kind", captureKind,
			"queue_wait_ms", queueWait.Milliseconds(),
		)
	}
	operationID := c.sequence.Add(1)
	active := c.active.Add(1)
	started := time.Now()
	attempts := 0
	frameWidth := 0
	frameHeight := 0
	c.logCaptureLifecycle(
		ctx,
		"wgc_capture_started",
		"operation_id", operationID,
		"capture_kind", captureKind,
		"include_cursor", includeCursor,
		"active_operations", active,
	)
	defer func() {
		activeAfter := c.active.Add(-1)
		attributes := []any{
			"operation_id", operationID,
			"capture_kind", captureKind,
			"attempts", attempts,
			"duration_ms", time.Since(started).Milliseconds(),
			"active_operations", activeAfter,
			"frame_width", frameWidth,
			"frame_height", frameHeight,
		}
		if returnErr != nil {
			attributes = append(attributes, "capture_error_code", captureErrorCode(returnErr), "error", returnErr)
			c.logger.ErrorContext(ctx, "wgc_capture_failed", attributes...)
			return
		}
		c.logCaptureLifecycle(ctx, "wgc_capture_completed", attributes...)
	}()

	wrappedProcess := func(frame capturedFrame) error {
		frameWidth = frame.width
		frameHeight = frame.height
		return process(frame)
	}
	_, returnErr = onWinRTThread(ctx, func() (struct{}, error) {
		supported, err := graphicsCaptureSupported()
		if err != nil {
			return struct{}{}, capture.Failure("capture_support_check_failed", "failed to query Windows Graphics Capture support", err)
		}
		if !supported {
			return struct{}{}, capture.Failure("capture_unsupported", "Windows Graphics Capture is not supported", nil)
		}

		target, err := findPrimaryDisplay()
		if err != nil {
			return struct{}{}, err
		}
		defer release(target.adapter)
		monitor := monitorFromDesc(target.desc)

		pixelFormat := uint32(dxgiFormatB8G8R8A8UNorm)
		pixelFormatName := "B8G8R8A8_UNORM"
		switch target.desc.ColorSpace {
		case dxgiColorSpaceRGBFullG22NoneP709:
		case dxgiColorSpaceRGBFullG2084NoneP2020:
			if !finitePositiveAbove80(float64(target.desc.MaxLuminance)) {
				return struct{}{}, capture.Failure(
					"invalid_hdr_metadata",
					"HDR display metadata must provide finite maximum luminance above 80 nits",
					nil,
				)
			}
			pixelFormat = dxgiFormatR16G16B16A16Float
			pixelFormatName = "R16G16B16A16_FLOAT"
		default:
			return struct{}{}, capture.Failure(
				"unsupported_color_space",
				fmt.Sprintf("unsupported primary-monitor color space: %s", colorSpaceName(target.desc.ColorSpace)),
				nil,
			)
		}

		device, context3D, winRTDevice, err := createD3DDevice(target.adapter)
		if err != nil {
			return struct{}{}, capture.Failure("capture_device_failed", "failed to create the Direct3D 11 capture device", err)
		}
		defer release(device)
		defer release(context3D)
		defer release(winRTDevice)

		err = retryTransientWGCCapture(
			ctx,
			wgcCaptureAttempts,
			wgcRetryDelay,
			func() error {
				attempts++
				c.logCaptureLifecycle(
					ctx,
					"wgc_capture_attempt_started",
					"operation_id", operationID,
					"capture_kind", captureKind,
					"attempt", attempts,
				)
				return captureFrameAttempt(
					ctx, target.desc, monitor, device, context3D, winRTDevice,
					pixelFormat, pixelFormatName, includeCursor, wrappedProcess,
				)
			},
			func(attempt int, err error) {
				c.logger.WarnContext(
					ctx,
					"retrying transient WGC capture failure",
					"operation_id", operationID,
					"capture_kind", captureKind,
					"attempt", attempt,
					"max_attempts", wgcCaptureAttempts,
					"error", err,
				)
			},
		)
		return struct{}{}, err
	})
	return returnErr
}

func (c *Capturer) logCaptureLifecycle(ctx context.Context, message string, attributes ...any) {
	if c.trace.Load() {
		c.logger.InfoContext(ctx, message, attributes...)
		return
	}
	c.logger.DebugContext(ctx, message, attributes...)
}

func captureErrorCode(err error) string {
	var captureErr *capture.Error
	if errors.As(err, &captureErr) {
		return captureErr.Code
	}
	return ""
}

func captureFrameAttempt(
	ctx context.Context,
	display outputDesc1,
	monitor capture.Monitor,
	device, context3D, winRTDevice unsafe.Pointer,
	pixelFormat uint32,
	pixelFormatName string,
	includeCursor bool,
	process func(capturedFrame) error,
) error {
	item, size, err := createMonitorItem(display.Monitor)
	if err != nil {
		return capture.Failure("desktop_unavailable", "failed to create a capture item for the primary monitor", err)
	}
	defer release(item)
	if size.Width <= 0 || size.Height <= 0 {
		return capture.Failure("desktop_unavailable", "primary monitor capture size is invalid", nil)
	}

	framePool, err := createFreeThreadedFramePool(winRTDevice, int32(pixelFormat), size)
	if err != nil {
		return capture.Failure("capture_session_failed", "failed to create the WGC frame pool", err)
	}
	defer closeAndRelease(framePool)

	session, err := createCaptureSession(framePool, item)
	if err != nil {
		return capture.Failure("capture_session_failed", "failed to create the WGC capture session", err)
	}
	defer closeAndRelease(session)
	if err := setCursorCapture(session, includeCursor); err != nil {
		return capture.Failure("capture_session_failed", "failed to configure cursor capture", err)
	}
	if err := callHRESULT(session, 6); err != nil {
		return capture.Failure("capture_session_failed", "failed to start WGC capture", err)
	}

	frame, err := waitForFrame(ctx, framePool)
	if err != nil {
		return err
	}
	defer closeAndRelease(frame)

	foregroundInfo, err := foreground.Snapshot()
	if err != nil {
		return capture.Failure(
			"foreground_process_unavailable",
			"failed to identify the foreground process at capture time",
			err,
		)
	}

	return process(capturedFrame{
		frame: frame, device: device, context3D: context3D,
		pixelFormat: pixelFormat, pixelFormatName: pixelFormatName,
		display: display, monitor: monitor, foreground: foregroundInfo,
		width: int(size.Width), height: int(size.Height),
	})
}

func onWinRTThread[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procRoInitialize.Call(uintptr(winapirt.RO_INIT_MULTITHREADED))
	if hr != 0 && hr != 1 {
		return zero, fmt.Errorf("RoInitialize(MTA): HRESULT %#08x", uint32(hr))
	}
	defer procRoUninitialize.Call()
	return operation()
}

func graphicsCaptureSupported() (bool, error) {
	const graphicsCaptureSessionClass = "Windows.Graphics.Capture.GraphicsCaptureSession"
	factory, err := ole.RoGetActivationFactory(graphicsCaptureSessionClass, winapirt.IGraphicsCaptureSessionStaticsID)
	if err != nil {
		return false, err
	}
	defer factory.Release()
	var supported uint8
	if err := callHRESULTWith(factoryPointer(factory), 6, uintptr(unsafe.Pointer(&supported))); err != nil {
		return false, err
	}
	return supported != 0, nil
}

func findPrimaryDisplay() (displayTarget, error) {
	primary, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	if primary == 0 {
		return displayTarget{}, capture.Failure("desktop_unavailable", "Windows did not return a primary monitor", nil)
	}

	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if err := checkHRESULT(hr, "CreateDXGIFactory1"); err != nil {
		return displayTarget{}, capture.Failure("capture_device_failed", "failed to create DXGI factory", err)
	}
	defer release(factory)

	for adapterIndex := uint32(0); ; adapterIndex++ {
		var adapter unsafe.Pointer
		hr, _, _ := syscall.SyscallN(
			comMethod(factory, 12),
			uintptr(factory),
			uintptr(adapterIndex),
			uintptr(unsafe.Pointer(&adapter)),
		)
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if err := checkHRESULT(hr, "IDXGIFactory1.EnumAdapters1"); err != nil {
			return displayTarget{}, capture.Failure("capture_device_failed", "failed to enumerate DXGI adapters", err)
		}

		matched := false
		for outputIndex := uint32(0); ; outputIndex++ {
			var output unsafe.Pointer
			hr, _, _ = syscall.SyscallN(
				comMethod(adapter, 7),
				uintptr(adapter),
				uintptr(outputIndex),
				uintptr(unsafe.Pointer(&output)),
			)
			if uint32(hr) == dxgiErrorNotFound {
				break
			}
			if err := checkHRESULT(hr, "IDXGIAdapter1.EnumOutputs"); err != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "failed to enumerate DXGI outputs", err)
			}

			var output6 unsafe.Pointer
			queryErr := queryInterface(output, iidIDXGIOutput6, &output6)
			release(output)
			if queryErr != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "primary output does not expose IDXGIOutput6", queryErr)
			}
			var desc outputDesc1
			hr, _, _ = syscall.SyscallN(
				comMethod(output6, 27),
				uintptr(output6),
				uintptr(unsafe.Pointer(&desc)),
			)
			release(output6)
			if err := checkHRESULT(hr, "IDXGIOutput6.GetDesc1"); err != nil {
				release(adapter)
				return displayTarget{}, capture.Failure("capture_device_failed", "failed to read DXGI output metadata", err)
			}
			if desc.Monitor == primary {
				if desc.AttachedToDesktop == 0 {
					release(adapter)
					return displayTarget{}, capture.Failure("desktop_unavailable", "primary monitor is not attached to the desktop", nil)
				}
				matched = true
				return displayTarget{adapter: adapter, desc: desc}, nil
			}
		}
		if !matched {
			release(adapter)
		}
	}
	return displayTarget{}, capture.Failure("desktop_unavailable", "primary monitor was not found in active DXGI outputs", nil)
}

func createD3DDevice(adapter unsafe.Pointer) (unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, error) {
	var device unsafe.Pointer
	var context3D unsafe.Pointer
	hr, _, _ := procD3D11CreateDevice.Call(
		uintptr(adapter),
		d3dDriverTypeUnknown,
		0,
		d3d11CreateDeviceBGRASupport,
		0,
		0,
		d3d11SDKVersion,
		uintptr(unsafe.Pointer(&device)),
		0,
		uintptr(unsafe.Pointer(&context3D)),
	)
	if err := checkHRESULT(hr, "D3D11CreateDevice"); err != nil {
		return nil, nil, nil, err
	}

	var dxgiDevice unsafe.Pointer
	if err := queryInterface(device, iidIDXGIDevice, &dxgiDevice); err != nil {
		release(context3D)
		release(device)
		return nil, nil, nil, fmt.Errorf("query IDXGIDevice: %w", err)
	}
	defer release(dxgiDevice)

	var winRTDevice unsafe.Pointer
	hr, _, _ = procCreateDirect3D11DeviceFromDXGIDevice.Call(
		uintptr(dxgiDevice),
		uintptr(unsafe.Pointer(&winRTDevice)),
	)
	if err := checkHRESULT(hr, "CreateDirect3D11DeviceFromDXGIDevice"); err != nil {
		release(context3D)
		release(device)
		return nil, nil, nil, err
	}
	return device, context3D, winRTDevice, nil
}

func createMonitorItem(monitor uintptr) (unsafe.Pointer, winapirt.SizeInt32, error) {
	factory, err := ole.RoGetActivationFactory(winapirt.GraphicsCaptureItemClass, winapirt.IGraphicsCaptureItemInteropID)
	if err != nil {
		return nil, winapirt.SizeInt32{}, err
	}
	defer factory.Release()

	var item unsafe.Pointer
	if err := callHRESULTWith(
		factoryPointer(factory),
		4,
		monitor,
		uintptr(unsafe.Pointer(winapirt.IGraphicsCaptureItemID)),
		uintptr(unsafe.Pointer(&item)),
	); err != nil {
		return nil, winapirt.SizeInt32{}, err
	}
	var size winapirt.SizeInt32
	if err := callHRESULTWith(item, 7, uintptr(unsafe.Pointer(&size))); err != nil {
		release(item)
		return nil, winapirt.SizeInt32{}, err
	}
	return item, size, nil
}

func createFreeThreadedFramePool(device unsafe.Pointer, pixelFormat int32, size winapirt.SizeInt32) (unsafe.Pointer, error) {
	return createFreeThreadedFramePoolWithBuffers(device, pixelFormat, size, 1)
}

func createFreeThreadedFramePoolWithBuffers(device unsafe.Pointer, pixelFormat int32, size winapirt.SizeInt32, buffers int) (unsafe.Pointer, error) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return nil, errors.New("SizeInt32 ABI wrapper requires windows/amd64")
	}
	if buffers < 1 || buffers > 8 {
		return nil, errors.New("WGC frame pool buffer count must be between 1 and 8")
	}
	packedSize := uint64(uint32(size.Width)) | uint64(uint32(size.Height))<<32
	if err := pixels.ValidatePackedSize(size.Width, size.Height, packedSize); err != nil {
		return nil, err
	}
	factory, err := ole.RoGetActivationFactory(winapirt.Direct3D11CaptureFramePoolClass, winapirt.IDirect3D11CaptureFramePoolStatics2ID)
	if err != nil {
		return nil, err
	}
	defer factory.Release()

	var framePool unsafe.Pointer
	if err := callHRESULTWith(
		factoryPointer(factory),
		6,
		uintptr(device),
		uintptr(pixelFormat),
		uintptr(buffers),
		uintptr(packedSize),
		uintptr(unsafe.Pointer(&framePool)),
	); err != nil {
		return nil, err
	}
	return framePool, nil
}

func createCaptureSession(framePool, item unsafe.Pointer) (unsafe.Pointer, error) {
	var session unsafe.Pointer
	if err := callHRESULTWith(
		framePool,
		10,
		uintptr(item),
		uintptr(unsafe.Pointer(&session)),
	); err != nil {
		return nil, err
	}
	return session, nil
}

func setCursorCapture(session unsafe.Pointer, include bool) error {
	var session2 unsafe.Pointer
	if err := queryInterface(session, winapirt.IGraphicsCaptureSession2ID, &session2); err != nil {
		return err
	}
	defer release(session2)
	value := uintptr(0)
	if include {
		value = 1
	}
	return callHRESULTWith(session2, 7, value)
}

func waitForFrame(ctx context.Context, framePool unsafe.Pointer) (unsafe.Pointer, error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var frame unsafe.Pointer
		if err := callHRESULTWith(framePool, 7, uintptr(unsafe.Pointer(&frame))); err != nil {
			return nil, capture.Failure("capture_frame_failed", "WGC failed while retrieving a frame", err)
		}
		if frame != nil {
			return frame, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, capture.Failure("capture_timeout", "timed out waiting for a WGC frame", ctx.Err())
			}
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func readRegionPixels(frame capturedFrame, physical capture.PixelRegion, outputWidth, outputHeight int) ([]uint32, error) {
	shader, err := createRegionComputeShader(frame.device)
	if err != nil {
		return nil, capture.Failure("capture_region_shader_failed", "failed to create the region sampling shader", err)
	}
	defer release(shader)
	return readRegionPixelsWithShader(frame, physical, outputWidth, outputHeight, shader)
}

func readRegionPixelsWithShader(frame capturedFrame, physical capture.PixelRegion, outputWidth, outputHeight int, shader unsafe.Pointer) ([]uint32, error) {
	if shader == nil {
		return nil, capture.Failure("capture_region_shader_failed", "region sampling shader is required", nil)
	}
	sourceTexture, sourceDesc, err := openFrameTexture(frame.frame, frame.pixelFormat)
	if err != nil {
		return nil, err
	}
	defer release(sourceTexture)
	if int(sourceDesc.Width) != frame.width || int(sourceDesc.Height) != frame.height {
		return nil, capture.Failure(
			"capture_size_mismatch",
			fmt.Sprintf("captured texture is %dx%d but WGC item is %dx%d", sourceDesc.Width, sourceDesc.Height, frame.width, frame.height),
			nil,
		)
	}

	inputDesc := texture2DDesc{
		Width: uint32(physical.Width), Height: uint32(physical.Height),
		MipLevels: 1, ArraySize: 1, Format: frame.pixelFormat,
		SampleDesc: sampleDesc{Count: 1}, Usage: d3d11UsageDefault,
		BindFlags: d3d11BindShaderResource,
	}
	var inputTexture unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 5,
		uintptr(unsafe.Pointer(&inputDesc)), 0, uintptr(unsafe.Pointer(&inputTexture)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to create the region source texture", err)
	}
	defer release(inputTexture)
	sourceBox := d3d11Box{
		Left: uint32(physical.Left), Top: uint32(physical.Top), Front: 0,
		Right: uint32(physical.Left + physical.Width), Bottom: uint32(physical.Top + physical.Height), Back: 1,
	}
	syscall.SyscallN(
		comMethod(frame.context3D, 46), uintptr(frame.context3D),
		uintptr(inputTexture), 0, 0, 0, 0,
		uintptr(sourceTexture), 0, uintptr(unsafe.Pointer(&sourceBox)),
	)

	outputDesc := texture2DDesc{
		Width: uint32(outputWidth), Height: uint32(outputHeight),
		MipLevels: 1, ArraySize: 1, Format: dxgiFormatR32Uint,
		SampleDesc: sampleDesc{Count: 1}, Usage: d3d11UsageDefault,
		BindFlags: d3d11BindUnorderedAccess,
	}
	var outputTexture unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 5,
		uintptr(unsafe.Pointer(&outputDesc)), 0, uintptr(unsafe.Pointer(&outputTexture)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to create the region output texture", err)
	}
	defer release(outputTexture)

	var sourceView unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 7,
		uintptr(inputTexture), 0, uintptr(unsafe.Pointer(&sourceView)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to create the region shader resource view", err)
	}
	defer release(sourceView)
	var outputView unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 8,
		uintptr(outputTexture), 0, uintptr(unsafe.Pointer(&outputView)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to create the region unordered-access view", err)
	}
	defer release(outputView)

	white := float32(1)
	hdr := uint32(0)
	if frame.monitor.HDR {
		ratio := float32(frame.display.MaxLuminance / 80)
		white = ratio * ratio
		hdr = 1
	}
	constants := regionShaderConstants{
		SourceWidth: uint32(physical.Width), SourceHeight: uint32(physical.Height),
		OutputWidth: uint32(outputWidth), OutputHeight: uint32(outputHeight),
		WhiteSquared: white, HDR: hdr,
	}
	constantDesc := bufferDesc{
		ByteWidth: uint32(unsafe.Sizeof(constants)), Usage: d3d11UsageDefault,
		BindFlags: d3d11BindConstantBuffer,
	}
	constantData := subresourceData{SystemMemory: unsafe.Pointer(&constants)}
	var constantBuffer unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 3,
		uintptr(unsafe.Pointer(&constantDesc)), uintptr(unsafe.Pointer(&constantData)),
		uintptr(unsafe.Pointer(&constantBuffer)),
	); err != nil {
		return nil, capture.Failure("capture_region_shader_failed", "failed to create the region shader constants", err)
	}
	defer release(constantBuffer)

	syscall.SyscallN(comMethod(frame.context3D, 67), uintptr(frame.context3D), 0, 1, uintptr(unsafe.Pointer(&sourceView)))
	syscall.SyscallN(comMethod(frame.context3D, 68), uintptr(frame.context3D), 0, 1, uintptr(unsafe.Pointer(&outputView)), 0)
	syscall.SyscallN(comMethod(frame.context3D, 69), uintptr(frame.context3D), uintptr(shader), 0, 0)
	syscall.SyscallN(comMethod(frame.context3D, 71), uintptr(frame.context3D), 0, 1, uintptr(unsafe.Pointer(&constantBuffer)))
	syscall.SyscallN(
		comMethod(frame.context3D, 41), uintptr(frame.context3D),
		uintptr((outputWidth+7)/8), uintptr((outputHeight+7)/8), 1,
	)
	var nullView unsafe.Pointer
	syscall.SyscallN(comMethod(frame.context3D, 67), uintptr(frame.context3D), 0, 1, uintptr(unsafe.Pointer(&nullView)))
	syscall.SyscallN(comMethod(frame.context3D, 68), uintptr(frame.context3D), 0, 1, uintptr(unsafe.Pointer(&nullView)), 0)
	syscall.SyscallN(comMethod(frame.context3D, 69), uintptr(frame.context3D), 0, 0, 0)

	stagingDesc := outputDesc
	stagingDesc.Usage = d3d11UsageStaging
	stagingDesc.BindFlags = 0
	stagingDesc.CPUAccessFlags = d3d11CPUAccessRead
	var staging unsafe.Pointer
	if err := callHRESULTWith(
		frame.device, 5,
		uintptr(unsafe.Pointer(&stagingDesc)), 0, uintptr(unsafe.Pointer(&staging)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to create the region staging texture", err)
	}
	defer release(staging)
	syscall.SyscallN(comMethod(frame.context3D, 47), uintptr(frame.context3D), uintptr(staging), uintptr(outputTexture))

	var mapped mappedSubresource
	if err := callHRESULTWith(
		frame.context3D, 14,
		uintptr(staging), 0, 1, 0, uintptr(unsafe.Pointer(&mapped)),
	); err != nil {
		return nil, capture.Failure("capture_readback_failed", "failed to map the region staging texture", err)
	}
	defer syscall.SyscallN(comMethod(frame.context3D, 15), uintptr(frame.context3D), uintptr(staging), 0)
	rowBytes := outputWidth * 4
	if mapped.Data == nil || int(mapped.RowPitch) < rowBytes {
		return nil, capture.Failure("capture_readback_failed", "mapped region texture has an invalid row pitch", nil)
	}
	bufferSize := uint64(mapped.RowPitch) * uint64(outputHeight)
	if bufferSize > uint64(math.MaxInt) {
		return nil, capture.Failure("capture_readback_failed", "mapped region texture is too large", nil)
	}
	raw := unsafe.Slice((*byte)(mapped.Data), int(bufferSize))
	pixels := make([]uint32, outputWidth*outputHeight)
	for y := 0; y < outputHeight; y++ {
		row := raw[y*int(mapped.RowPitch):]
		for x := 0; x < outputWidth; x++ {
			pixels[y*outputWidth+x] = uint32(row[x*4]) |
				uint32(row[x*4+1])<<8 |
				uint32(row[x*4+2])<<16 |
				uint32(row[x*4+3])<<24
		}
	}
	return pixels, nil
}

func createRegionComputeShader(device unsafe.Pointer) (unsafe.Pointer, error) {
	source := []byte(regionShaderSource)
	entrypoint := append([]byte("main"), 0)
	target := append([]byte("cs_5_0"), 0)
	sourceName := append([]byte("windows-agent-region.hlsl"), 0)
	var codeBlob unsafe.Pointer
	var errorBlob unsafe.Pointer
	hr, _, _ := procD3DCompile.Call(
		uintptr(unsafe.Pointer(&source[0])), uintptr(len(source)), uintptr(unsafe.Pointer(&sourceName[0])),
		0, 0, uintptr(unsafe.Pointer(&entrypoint[0])), uintptr(unsafe.Pointer(&target[0])),
		0, 0, uintptr(unsafe.Pointer(&codeBlob)), uintptr(unsafe.Pointer(&errorBlob)),
	)
	if errorBlob != nil {
		defer release(errorBlob)
	}
	if err := checkHRESULT(hr, "D3DCompile(region shader)"); err != nil {
		return nil, err
	}
	if codeBlob == nil {
		return nil, errors.New("D3DCompile returned no region shader bytecode")
	}
	defer release(codeBlob)
	codePointer, _, _ := syscall.SyscallN(comMethod(codeBlob, 3), uintptr(codeBlob))
	codeSize, _, _ := syscall.SyscallN(comMethod(codeBlob, 4), uintptr(codeBlob))
	if codePointer == 0 || codeSize == 0 {
		return nil, errors.New("compiled region shader bytecode is empty")
	}
	var shader unsafe.Pointer
	if err := callHRESULTWith(
		device, 18,
		codePointer, codeSize, 0, uintptr(unsafe.Pointer(&shader)),
	); err != nil {
		return nil, err
	}
	return shader, nil
}

func openFrameTexture(frame unsafe.Pointer, pixelFormat uint32) (unsafe.Pointer, texture2DDesc, error) {
	var surface unsafe.Pointer
	if err := callHRESULTWith(frame, 6, uintptr(unsafe.Pointer(&surface))); err != nil {
		return nil, texture2DDesc{}, capture.Failure("capture_frame_failed", "failed to access the WGC frame surface", err)
	}
	defer release(surface)
	var access unsafe.Pointer
	if err := queryInterface(surface, iidDXGIInterface, &access); err != nil {
		return nil, texture2DDesc{}, capture.Failure("capture_frame_failed", "frame surface does not expose IDirect3DDxgiInterfaceAccess", err)
	}
	defer release(access)
	var sourceTexture unsafe.Pointer
	if err := callHRESULTWith(
		access, 3,
		uintptr(unsafe.Pointer(iidTexture2D)), uintptr(unsafe.Pointer(&sourceTexture)),
	); err != nil {
		return nil, texture2DDesc{}, capture.Failure("capture_frame_failed", "failed to obtain the D3D11 frame texture", err)
	}
	var sourceDesc texture2DDesc
	syscall.SyscallN(comMethod(sourceTexture, 10), uintptr(sourceTexture), uintptr(unsafe.Pointer(&sourceDesc)))
	if sourceDesc.Width == 0 || sourceDesc.Height == 0 {
		release(sourceTexture)
		return nil, texture2DDesc{}, capture.Failure("capture_frame_failed", "captured D3D11 texture has invalid dimensions", nil)
	}
	if sourceDesc.Format != pixelFormat {
		release(sourceTexture)
		return nil, texture2DDesc{}, capture.Failure(
			"capture_format_mismatch",
			fmt.Sprintf("captured D3D11 texture format is %d, expected %d", sourceDesc.Format, pixelFormat),
			nil,
		)
	}
	return sourceTexture, sourceDesc, nil
}

func readFrame(frame, device, context3D unsafe.Pointer, pixelFormat uint32, desc outputDesc1) (*image.NRGBA, int, int, error) {
	var surface unsafe.Pointer
	if err := callHRESULTWith(frame, 6, uintptr(unsafe.Pointer(&surface))); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "failed to access the WGC frame surface", err)
	}
	defer release(surface)

	var access unsafe.Pointer
	if err := queryInterface(surface, iidDXGIInterface, &access); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "frame surface does not expose IDirect3DDxgiInterfaceAccess", err)
	}
	defer release(access)

	var sourceTexture unsafe.Pointer
	if err := callHRESULTWith(
		access,
		3,
		uintptr(unsafe.Pointer(iidTexture2D)),
		uintptr(unsafe.Pointer(&sourceTexture)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "failed to obtain the D3D11 frame texture", err)
	}
	defer release(sourceTexture)

	var sourceDesc texture2DDesc
	syscall.SyscallN(
		comMethod(sourceTexture, 10),
		uintptr(sourceTexture),
		uintptr(unsafe.Pointer(&sourceDesc)),
	)
	if sourceDesc.Width == 0 || sourceDesc.Height == 0 {
		return nil, 0, 0, capture.Failure("capture_frame_failed", "captured D3D11 texture has invalid dimensions", nil)
	}
	if sourceDesc.Format != pixelFormat {
		return nil, 0, 0, capture.Failure(
			"capture_format_mismatch",
			fmt.Sprintf("captured D3D11 texture format is %d, expected %d", sourceDesc.Format, pixelFormat),
			nil,
		)
	}

	stagingDesc := texture2DDesc{
		Width:          sourceDesc.Width,
		Height:         sourceDesc.Height,
		MipLevels:      1,
		ArraySize:      1,
		Format:         sourceDesc.Format,
		SampleDesc:     sampleDesc{Count: 1},
		Usage:          d3d11UsageStaging,
		CPUAccessFlags: d3d11CPUAccessRead,
	}
	var staging unsafe.Pointer
	if err := callHRESULTWith(
		device,
		5,
		uintptr(unsafe.Pointer(&stagingDesc)),
		0,
		uintptr(unsafe.Pointer(&staging)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to create the D3D11 staging texture", err)
	}
	defer release(staging)

	syscall.SyscallN(comMethod(context3D, 47), uintptr(context3D), uintptr(staging), uintptr(sourceTexture))
	var mapped mappedSubresource
	if err := callHRESULTWith(
		context3D,
		14,
		uintptr(staging),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&mapped)),
	); err != nil {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to map the D3D11 staging texture", err)
	}
	defer syscall.SyscallN(comMethod(context3D, 15), uintptr(context3D), uintptr(staging), 0)
	if mapped.Data == nil || mapped.RowPitch == 0 {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "mapped D3D11 texture returned an empty buffer", nil)
	}

	bufferSize := uint64(mapped.RowPitch) * uint64(sourceDesc.Height)
	if bufferSize > uint64(math.MaxInt) {
		return nil, 0, 0, capture.Failure("capture_readback_failed", "mapped D3D11 texture is too large", nil)
	}
	raw := make([]byte, int(bufferSize))
	copy(raw, unsafe.Slice((*byte)(mapped.Data), int(bufferSize)))
	width := int(sourceDesc.Width)
	height := int(sourceDesc.Height)

	switch pixelFormat {
	case dxgiFormatB8G8R8A8UNorm:
		converted, convertErr := pixels.BGRA8ToNRGBA(raw, width, height, int(mapped.RowPitch))
		if convertErr != nil {
			return nil, 0, 0, capture.Failure("capture_readback_failed", "failed to convert the SDR frame", convertErr)
		}
		return converted, width, height, nil
	case dxgiFormatR16G16B16A16Float:
		converted, convertErr := pixels.RGBA16FToToneMappedNRGBA(raw, width, height, int(mapped.RowPitch), float64(desc.MaxLuminance))
		if convertErr != nil {
			return nil, 0, 0, capture.Failure("capture_tone_map_failed", "failed to tone-map the HDR frame", convertErr)
		}
		return converted, width, height, nil
	default:
		return nil, 0, 0, capture.Failure("unsupported_capture_format", fmt.Sprintf("unsupported capture format: %d", pixelFormat), nil)
	}
}

func monitorFromDesc(desc outputDesc1) capture.Monitor {
	width := int(desc.DesktopCoordinates.Right - desc.DesktopCoordinates.Left)
	height := int(desc.DesktopCoordinates.Bottom - desc.DesktopCoordinates.Top)
	return capture.Monitor{
		DeviceName:       windows.UTF16ToString(desc.DeviceName[:]),
		Width:            width,
		Height:           height,
		HDR:              desc.ColorSpace == dxgiColorSpaceRGBFullG2084NoneP2020,
		ColorSpace:       colorSpaceName(desc.ColorSpace),
		MaxLuminanceNits: float64(desc.MaxLuminance),
	}
}

func colorSpaceName(value int32) string {
	switch value {
	case dxgiColorSpaceRGBFullG22NoneP709:
		return "RGB_FULL_G22_NONE_P709"
	case dxgiColorSpaceRGBFullG10NoneP709:
		return "RGB_FULL_G10_NONE_P709"
	case dxgiColorSpaceRGBFullG2084NoneP2020:
		return "RGB_FULL_G2084_NONE_P2020"
	default:
		return fmt.Sprintf("DXGI_COLOR_SPACE_%d", value)
	}
}

func finitePositiveAbove80(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 80
}

func queryInterface(object unsafe.Pointer, iid *ole.GUID, result *unsafe.Pointer) error {
	if object == nil {
		return errors.New("QueryInterface object is nil")
	}
	return callHRESULTWith(
		object,
		0,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(result)),
	)
}

func closeAndRelease(object unsafe.Pointer) {
	if object == nil {
		return
	}
	var closable unsafe.Pointer
	if queryInterface(object, iidClosable, &closable) == nil {
		_ = callHRESULT(closable, 6)
		release(closable)
	}
	release(object)
}

func release(object unsafe.Pointer) {
	if object == nil {
		return
	}
	syscall.SyscallN(comMethod(object, 2), uintptr(object))
}

func callHRESULT(object unsafe.Pointer, index int) error {
	return callHRESULTWith(object, index)
}

func callHRESULTWith(object unsafe.Pointer, index int, args ...uintptr) error {
	if object == nil {
		return errors.New("COM object is nil")
	}
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(object))
	callArgs = append(callArgs, args...)
	hr, _, _ := syscall.SyscallN(comMethod(object, index), callArgs...)
	return checkHRESULT(hr, fmt.Sprintf("COM method %d", index))
}

func comMethod(object unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(object)
	return (*[128]uintptr)(vtable)[index]
}

func checkHRESULT(value uintptr, operation string) error {
	if int32(value) < 0 {
		return fmt.Errorf("%s: HRESULT %#08x", operation, uint32(value))
	}
	return nil
}

func factoryPointer(factory *ole.IInspectable) unsafe.Pointer {
	return unsafe.Pointer(factory)
}
