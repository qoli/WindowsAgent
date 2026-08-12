//go:build windows && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/qoli/WindowsAgent/internal/actionosd"
	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/captureindicator"
	"github.com/qoli/WindowsAgent/internal/eventclient"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"github.com/qoli/WindowsAgent/internal/recordingindicator"
	"golang.org/x/sys/windows"
)

const (
	windowWidth        = int32(360)
	windowHeight       = int32(110)
	dotWidth           = int32(18)
	dotHeight          = int32(24)
	indicatorSlotWidth = int32(20)
	actionOffset       = int32(40)
	margin             = int32(16)

	lwaColorKey          = 0x00000001
	lwaAlpha             = 0x00000002
	wmNCHitTest          = 0x0084
	wmModelChanged       = win.WM_APP + 1
	wdExcludeFromCapture = 0x00000011
	timerID              = 1
	timerInterval        = 100 * time.Millisecond
	streamRetryDelay     = 2 * time.Second
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	setLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	setWindowDisplayAffinity   = user32.NewProc("SetWindowDisplayAffinity")

	backgroundColor win.COLORREF = rgb(255, 0, 255)
	primaryColor    win.COLORREF = rgb(242, 246, 252)
	secondaryColor  win.COLORREF = rgb(160, 174, 192)
	dimColor        win.COLORREF = rgb(101, 115, 134)
	liveColor       win.COLORREF = rgb(255, 70, 76)
	doneColor       win.COLORREF = rgb(64, 215, 132)
	warningColor    win.COLORREF = rgb(255, 184, 72)
	recordingColor  win.COLORREF = rgb(255, 204, 0)
	captureColor    win.COLORREF = rgb(57, 210, 255)

	activeWindow *overlayWindow
)

type config struct {
	EventAPIURL        string
	EventTokenFile     string
	LogFile            string
	AllowCapture       bool
	MinimumEventCursor uint64
}

type overlayWindow struct {
	hwnd             win.HWND
	dotHWND          win.HWND
	recordingDotHWND win.HWND
	captureDotHWND   win.HWND
	model            *actionosd.Model
	dotOn            bool
	recording        bool
	capturing        bool
	timerTicks       uint8
	failureMu        sync.Mutex
	failure          error
	closeOnce        sync.Once
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Getenv("LOCALAPPDATA"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "windows-action-osd:", err)
		os.Exit(1)
	}
	logger, closeLog, err := newLogger(cfg.LogFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "windows-action-osd:", err)
		os.Exit(1)
	}
	defer closeLog()
	if err := run(cfg, logger); err != nil {
		logger.Error("action_osd_failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, logger *slog.Logger) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	httpClient := &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          2,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}}
	client, err := eventclient.New(cfg.EventAPIURL, cfg.EventTokenFile, httpClient)
	if err != nil {
		return fmt.Errorf("initialize event client: %w", err)
	}
	startupContext, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startupCancel()
	if err := client.Health(startupContext); err != nil {
		return fmt.Errorf("require event service: %w", err)
	}
	last, err := client.LastSequence(startupContext)
	if err != nil {
		return fmt.Errorf("resolve current event cursor: %w", err)
	}
	after, err := actionosd.StartupCursor(last, cfg.MinimumEventCursor, eventstream.DefaultReplayLimit)
	if err != nil {
		return err
	}
	model := &actionosd.Model{}
	for after < last {
		events, next, currentLast, err := client.Replay(startupContext, after, eventstream.DefaultReplayLimit)
		if err != nil {
			return fmt.Errorf("reconstruct current Action OSD state: %w", err)
		}
		for _, event := range events {
			if err := model.Apply(event); err != nil {
				return fmt.Errorf("reconstruct Action OSD event %d: %w", event.Sequence, err)
			}
		}
		if next <= after {
			return errors.New("event replay did not advance its cursor")
		}
		after = next
		last = currentLast
	}

	window, err := newOverlayWindow(model, cfg.AllowCapture)
	if err != nil {
		return err
	}
	defer window.destroy()
	logger.Info("action_osd_started", "event_api_url", cfg.EventAPIURL, "after_cursor", after,
		"minimum_event_cursor", cfg.MinimumEventCursor, "capture_excluded", !cfg.AllowCapture)
	if err := window.refresh(); err != nil {
		return err
	}

	streamContext, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	go func() {
		retryCount := 0
		for streamContext.Err() == nil {
			err := client.Stream(streamContext, after, func(event eventstream.Event) error {
				if err := window.model.Apply(event); err != nil {
					return err
				}
				if retryCount > 0 {
					logger.Info("action_osd_stream_reconnected",
						"sequence", event.Sequence,
						"retry_count", retryCount,
					)
				}
				after = event.Sequence
				retryCount = 0
				if event.Stream == actionrun.StreamName {
					win.PostMessage(window.hwnd, wmModelChanged, 0, 0)
				}
				return nil
			})
			if streamContext.Err() != nil {
				return
			}
			retryCount++
			logger.Warn("action_osd_stream_disconnected",
				"error", err,
				"after_cursor", after,
				"retry_count", retryCount,
				"retry_in_ms", streamRetryDelay.Milliseconds(),
			)
			timer := time.NewTimer(streamRetryDelay)
			select {
			case <-streamContext.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()

	var message win.MSG
	for {
		status := win.GetMessage(&message, 0, 0, 0)
		if status == 0 {
			if err := window.runtimeFailure(); err != nil {
				return err
			}
			logger.Info("action_osd_stopped")
			return nil
		}
		if status == -1 {
			return errorsFromLast("read window message")
		}
		win.TranslateMessage(&message)
		win.DispatchMessage(&message)
	}
}

func parseConfig(args []string, localAppData string) (config, error) {
	if localAppData == "" {
		return config{}, errors.New("LOCALAPPDATA is required")
	}
	dataDir := filepath.Join(localAppData, "gameGuide", "windows-capture-agent")
	flags := flag.NewFlagSet("windows-action-osd", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var cfg config
	flags.StringVar(&cfg.EventAPIURL, "event-api-url", "http://127.0.0.1:8788", "loopback windows-event-stream HTTP origin")
	flags.StringVar(&cfg.EventTokenFile, "event-token-file", filepath.Join(dataDir, "event-api.token"), "absolute event API token file")
	flags.StringVar(&cfg.LogFile, "log-file", filepath.Join(dataDir, "logs", "action-osd.jsonl"), "absolute JSON log file")
	flags.BoolVar(&cfg.AllowCapture, "allow-capture", false, "allow screen capture to include the OSD")
	flags.Uint64Var(&cfg.MinimumEventCursor, "minimum-event-cursor", 0, "explicit lower bound for startup event reconstruction")
	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	if cfg.EventTokenFile == "" || !filepath.IsAbs(cfg.EventTokenFile) {
		return config{}, errors.New("--event-token-file must be an absolute path")
	}
	if cfg.LogFile == "" || !filepath.IsAbs(cfg.LogFile) {
		return config{}, errors.New("--log-file must be an absolute path")
	}
	return cfg, nil
}

func newLogger(path string) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create OSD log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open OSD log: %w", err)
	}
	return slog.New(slog.NewJSONHandler(file, nil)), func() { file.Close() }, nil
}

func newOverlayWindow(model *actionosd.Model, allowCapture bool) (*overlayWindow, error) {
	instance := win.GetModuleHandle(nil)
	if instance == 0 {
		return nil, windows.GetLastError()
	}
	className, _ := windows.UTF16PtrFromString("WindowsAgentActionOSD")
	windowName, _ := windows.UTF16PtrFromString("WindowsAgent Action OSD")
	backgroundBrush := solidBrush(backgroundColor)
	if backgroundBrush == 0 {
		return nil, errorsFromLast("create OSD background brush")
	}
	wndClass := win.WNDCLASSEX{
		CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: windows.NewCallback(windowProc),
		HInstance: instance, HbrBackground: backgroundBrush, LpszClassName: className,
	}
	if win.RegisterClassEx(&wndClass) == 0 {
		win.DeleteObject(win.HGDIOBJ(backgroundBrush))
		return nil, errorsFromLast("register OSD window class")
	}
	x, y := overlayPosition(win.GetForegroundWindow())
	exStyle := uint32(win.WS_EX_TOPMOST | win.WS_EX_LAYERED | win.WS_EX_TRANSPARENT | win.WS_EX_TOOLWINDOW | win.WS_EX_NOACTIVATE)
	hwnd := win.CreateWindowEx(exStyle, className, windowName, win.WS_POPUP, x+actionOffset, y, windowWidth, windowHeight, 0, 0, instance, nil)
	if hwnd == 0 {
		win.UnregisterClass(className)
		win.DeleteObject(win.HGDIOBJ(backgroundBrush))
		return nil, errorsFromLast("create OSD window")
	}
	layered, _, _ := setLayeredWindowAttributes.Call(uintptr(hwnd), uintptr(backgroundColor), 255, lwaColorKey)
	if layered == 0 {
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("configure OSD layered window")
	}
	if !allowCapture {
		excluded, _, _ := setWindowDisplayAffinity.Call(uintptr(hwnd), wdExcludeFromCapture)
		if excluded == 0 {
			win.DestroyWindow(hwnd)
			return nil, errorsFromLast("exclude OSD from capture")
		}
	}
	dotHWND := win.CreateWindowEx(exStyle, className, windowName, win.WS_POPUP, x+actionOffset, y, dotWidth, dotHeight, 0, 0, instance, nil)
	if dotHWND == 0 {
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("create OSD pulse window")
	}
	dotLayered, _, _ := setLayeredWindowAttributes.Call(uintptr(dotHWND), uintptr(backgroundColor), 255, lwaColorKey|lwaAlpha)
	if dotLayered == 0 {
		win.DestroyWindow(dotHWND)
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("configure OSD pulse window")
	}
	if !allowCapture {
		excluded, _, _ := setWindowDisplayAffinity.Call(uintptr(dotHWND), wdExcludeFromCapture)
		if excluded == 0 {
			win.DestroyWindow(dotHWND)
			win.DestroyWindow(hwnd)
			return nil, errorsFromLast("exclude OSD pulse from capture")
		}
	}
	recordingDotHWND := win.CreateWindowEx(exStyle, className, windowName, win.WS_POPUP, x, y, dotWidth, dotHeight, 0, 0, instance, nil)
	if recordingDotHWND == 0 {
		win.DestroyWindow(dotHWND)
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("create Evidence recording indicator window")
	}
	recordingLayered, _, _ := setLayeredWindowAttributes.Call(uintptr(recordingDotHWND), uintptr(backgroundColor), 255, lwaColorKey|lwaAlpha)
	if recordingLayered == 0 {
		win.DestroyWindow(recordingDotHWND)
		win.DestroyWindow(dotHWND)
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("configure Evidence recording indicator window")
	}
	if !allowCapture {
		excluded, _, _ := setWindowDisplayAffinity.Call(uintptr(recordingDotHWND), wdExcludeFromCapture)
		if excluded == 0 {
			win.DestroyWindow(recordingDotHWND)
			win.DestroyWindow(dotHWND)
			win.DestroyWindow(hwnd)
			return nil, errorsFromLast("exclude Evidence recording indicator from capture")
		}
	}
	captureDotHWND := win.CreateWindowEx(exStyle, className, windowName, win.WS_POPUP, x, y, dotWidth, dotHeight, 0, 0, instance, nil)
	if captureDotHWND == 0 {
		win.DestroyWindow(recordingDotHWND)
		win.DestroyWindow(dotHWND)
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("create capture activity indicator window")
	}
	captureLayered, _, _ := setLayeredWindowAttributes.Call(uintptr(captureDotHWND), uintptr(backgroundColor), 255, lwaColorKey|lwaAlpha)
	if captureLayered == 0 {
		win.DestroyWindow(captureDotHWND)
		win.DestroyWindow(recordingDotHWND)
		win.DestroyWindow(dotHWND)
		win.DestroyWindow(hwnd)
		return nil, errorsFromLast("configure capture activity indicator window")
	}
	if !allowCapture {
		excluded, _, _ := setWindowDisplayAffinity.Call(uintptr(captureDotHWND), wdExcludeFromCapture)
		if excluded == 0 {
			win.DestroyWindow(captureDotHWND)
			win.DestroyWindow(recordingDotHWND)
			win.DestroyWindow(dotHWND)
			win.DestroyWindow(hwnd)
			return nil, errorsFromLast("exclude capture activity indicator from capture")
		}
	}
	window := &overlayWindow{hwnd: hwnd, dotHWND: dotHWND, recordingDotHWND: recordingDotHWND, captureDotHWND: captureDotHWND, model: model, dotOn: true}
	activeWindow = window
	win.SetTimer(hwnd, timerID, uint32(timerInterval.Milliseconds()), 0)
	return window, nil
}

func (w *overlayWindow) destroy() {
	w.closeOnce.Do(func() {
		if w.captureDotHWND != 0 {
			win.DestroyWindow(w.captureDotHWND)
			w.captureDotHWND = 0
		}
		if w.recordingDotHWND != 0 {
			win.DestroyWindow(w.recordingDotHWND)
			w.recordingDotHWND = 0
		}
		if w.dotHWND != 0 {
			win.DestroyWindow(w.dotHWND)
			w.dotHWND = 0
		}
		if w.hwnd != 0 {
			win.KillTimer(w.hwnd, timerID)
			win.DestroyWindow(w.hwnd)
			w.hwnd = 0
		}
		activeWindow = nil
	})
}

func windowProc(hwnd win.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case win.WM_PAINT:
		if activeWindow != nil {
			activeWindow.paint(hwnd)
		}
		return 0
	case win.WM_TIMER:
		if activeWindow != nil {
			activeWindow.timerTicks++
			if activeWindow.timerTicks == 10 {
				activeWindow.timerTicks = 0
				activeWindow.toggleDot()
			}
			if err := activeWindow.refresh(); err != nil {
				activeWindow.fail(err)
			}
		}
		return 0
	case wmModelChanged:
		if activeWindow != nil {
			if err := activeWindow.refresh(); err != nil {
				activeWindow.fail(err)
			}
		}
		return 0
	case wmNCHitTest:
		return uintptr(^uint32(0))
	case win.WM_CLOSE:
		win.DestroyWindow(hwnd)
		return 0
	case win.WM_DESTROY:
		if activeWindow != nil {
			if activeWindow.recordingDotHWND == hwnd {
				activeWindow.recordingDotHWND = 0
				return 0
			}
			if activeWindow.captureDotHWND == hwnd {
				activeWindow.captureDotHWND = 0
				return 0
			}
			if activeWindow.dotHWND == hwnd {
				activeWindow.dotHWND = 0
				return 0
			}
			if activeWindow.hwnd == hwnd {
				activeWindow.hwnd = 0
			}
		}
		win.PostQuitMessage(0)
		return 0
	default:
		return win.DefWindowProc(hwnd, message, wParam, lParam)
	}
}

func (w *overlayWindow) refresh() error {
	recording, err := recordingindicator.Active()
	if err != nil {
		return fmt.Errorf("read Evidence recording state: %w", err)
	}
	w.recording = recording
	capturing, err := captureindicator.Active()
	if err != nil {
		return fmt.Errorf("read capture activity state: %w", err)
	}
	w.capturing = capturing
	snapshot := w.model.Snapshot(time.Now().UTC())
	if !snapshot.Visible {
		win.ShowWindow(w.hwnd, win.SW_HIDE)
		win.ShowWindow(w.dotHWND, win.SW_HIDE)
	}
	x, y := overlayPosition(win.GetForegroundWindow())
	if capturing {
		win.SetWindowPos(w.captureDotHWND, win.HWND_TOPMOST, x, y, dotWidth, dotHeight, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
		win.InvalidateRect(w.captureDotHWND, nil, true)
	} else {
		win.ShowWindow(w.captureDotHWND, win.SW_HIDE)
	}
	if recording {
		win.SetWindowPos(w.recordingDotHWND, win.HWND_TOPMOST, x+indicatorSlotWidth, y, dotWidth, dotHeight, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
		win.InvalidateRect(w.recordingDotHWND, nil, true)
	} else {
		win.ShowWindow(w.recordingDotHWND, win.SW_HIDE)
	}
	if !snapshot.Visible {
		return nil
	}
	win.SetWindowPos(w.hwnd, win.HWND_TOPMOST, x+actionOffset, y, windowWidth, windowHeight, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	win.SetWindowPos(w.dotHWND, win.HWND_TOPMOST, x+actionOffset, y, dotWidth, dotHeight, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	alpha := byte(255)
	if snapshot.Status == actionosd.StatusLive && !w.dotOn {
		alpha = 0
	}
	setLayeredWindowAttributes.Call(uintptr(w.dotHWND), uintptr(backgroundColor), uintptr(alpha), lwaColorKey|lwaAlpha)
	win.InvalidateRect(w.hwnd, nil, true)
	win.InvalidateRect(w.dotHWND, nil, true)
	return nil
}

func (w *overlayWindow) paint(hwnd win.HWND) {
	if hwnd == w.captureDotHWND {
		w.paintCaptureDot()
		return
	}
	if hwnd == w.recordingDotHWND {
		w.paintRecordingDot()
		return
	}
	if hwnd == w.dotHWND {
		w.paintDot()
		return
	}
	w.paintText()
}

func (w *overlayWindow) paintCaptureDot() {
	var paint win.PAINTSTRUCT
	hdc := win.BeginPaint(w.captureDotHWND, &paint)
	if hdc == 0 {
		return
	}
	defer win.EndPaint(w.captureDotHWND, &paint)
	if w.capturing {
		paintCircle(hdc, captureColor)
	}
}

func (w *overlayWindow) paintRecordingDot() {
	var paint win.PAINTSTRUCT
	hdc := win.BeginPaint(w.recordingDotHWND, &paint)
	if hdc == 0 {
		return
	}
	defer win.EndPaint(w.recordingDotHWND, &paint)
	if !w.recording {
		return
	}
	paintCircle(hdc, recordingColor)
}

func (w *overlayWindow) paintText() {
	snapshot := w.model.Snapshot(time.Now().UTC())
	var paint win.PAINTSTRUCT
	hdc := win.BeginPaint(w.hwnd, &paint)
	if hdc == 0 {
		return
	}
	defer win.EndPaint(w.hwnd, &paint)
	if !snapshot.Visible {
		return
	}
	win.SetBkMode(hdc, win.TRANSPARENT)
	drawText(hdc, displayActionName(snapshot.ActionID), 22, 1, 354, 27, 15, win.FW_BOLD, primaryColor, win.DT_LEFT|win.DT_END_ELLIPSIS)
	for index, activity := range snapshot.Activities {
		y := int32(32 + index*24)
		color := dimColor
		if index == len(snapshot.Activities)-1 {
			color = activityColor(activity.Level)
		} else if index == len(snapshot.Activities)-2 {
			color = secondaryColor
		}
		drawText(hdc, activity.Message, 2, y, 354, y+21, 13, win.FW_NORMAL, color, win.DT_LEFT|win.DT_END_ELLIPSIS)
	}
}

func (w *overlayWindow) paintDot() {
	snapshot := w.model.Snapshot(time.Now().UTC())
	var paint win.PAINTSTRUCT
	hdc := win.BeginPaint(w.dotHWND, &paint)
	if hdc == 0 {
		return
	}
	defer win.EndPaint(w.dotHWND, &paint)
	if !snapshot.Visible {
		return
	}
	paintCircle(hdc, colorForStatus(snapshot.Status))
}

func paintCircle(hdc win.HDC, color win.COLORREF) {
	brush := solidBrush(color)
	if brush == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(brush))
	previousPen := win.SelectObject(hdc, win.GetStockObject(win.NULL_PEN))
	defer win.SelectObject(hdc, previousPen)
	previousBrush := win.SelectObject(hdc, win.HGDIOBJ(brush))
	win.Ellipse(hdc, 2, 6, 16, 20)
	win.SelectObject(hdc, previousBrush)
}

func (w *overlayWindow) fail(err error) {
	w.failureMu.Lock()
	if w.failure == nil {
		w.failure = err
	}
	w.failureMu.Unlock()
	win.PostMessage(w.hwnd, win.WM_CLOSE, 0, 0)
}

func (w *overlayWindow) runtimeFailure() error {
	w.failureMu.Lock()
	defer w.failureMu.Unlock()
	return w.failure
}

func displayActionName(actionID string) string {
	if separator := strings.LastIndexByte(actionID, '/'); separator >= 0 && separator+1 < len(actionID) {
		return actionID[separator+1:]
	}
	return actionID
}

func (w *overlayWindow) toggleDot() {
	w.dotOn = !w.dotOn
}

func overlayPosition(foreground win.HWND) (int32, int32) {
	monitor := win.MonitorFromWindow(foreground, win.MONITOR_DEFAULTTOPRIMARY)
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if monitor != 0 && win.GetMonitorInfo(monitor, &info) {
		return info.RcWork.Left + margin, info.RcWork.Top + margin
	}
	return margin, margin
}

func colorForStatus(status string) win.COLORREF {
	switch status {
	case actionosd.StatusDone:
		return doneColor
	case actionosd.StatusFailed:
		return liveColor
	case actionosd.StatusStopped:
		return secondaryColor
	default:
		return liveColor
	}
}

func activityColor(level string) win.COLORREF {
	switch level {
	case "warning":
		return warningColor
	case "error":
		return liveColor
	default:
		return primaryColor
	}
}

func drawText(hdc win.HDC, value string, left, top, right, bottom, height, weight int32, color win.COLORREF, format uint32) {
	font := win.CreateFontIndirect(&win.LOGFONT{
		LfHeight: -height, LfWeight: weight, LfCharSet: win.DEFAULT_CHARSET,
		LfQuality: win.NONANTIALIASED_QUALITY, LfFaceName: utf16Face("Segoe UI"),
	})
	if font == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(font))
	previous := win.SelectObject(hdc, win.HGDIOBJ(font))
	defer win.SelectObject(hdc, previous)
	win.SetTextColor(hdc, color)
	text, _ := windows.UTF16PtrFromString(value)
	rect := win.RECT{Left: left, Top: top, Right: right, Bottom: bottom}
	win.DrawTextEx(hdc, text, -1, &rect, format|win.DT_SINGLELINE|win.DT_NOPREFIX, nil)
}

func utf16Face(value string) [win.LF_FACESIZE]uint16 {
	encoded, _ := windows.UTF16FromString(value)
	var result [win.LF_FACESIZE]uint16
	copy(result[:], encoded)
	return result
}

func solidBrush(color win.COLORREF) win.HBRUSH {
	return win.CreateBrushIndirect(&win.LOGBRUSH{LbStyle: win.BS_SOLID, LbColor: color})
}

func rgb(red, green, blue byte) win.COLORREF {
	return win.COLORREF(uint32(red) | uint32(green)<<8 | uint32(blue)<<16)
}

func errorsFromLast(operation string) error {
	err := windows.GetLastError()
	if err == nil {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
