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
	"sync"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/qoli/WindowsAgent/internal/actionosd"
	"github.com/qoli/WindowsAgent/internal/actionrun"
	"github.com/qoli/WindowsAgent/internal/eventclient"
	"github.com/qoli/WindowsAgent/internal/eventstream"
	"golang.org/x/sys/windows"
)

const (
	windowWidth  = int32(520)
	windowHeight = int32(214)
	margin       = int32(28)

	lwaColorKey = 0x00000001
	lwaAlpha    = 0x00000002

	wmNCHitTest          = 0x0084
	wmModelChanged       = win.WM_APP + 1
	wmStreamFailed       = win.WM_APP + 2
	wdExcludeFromCapture = 0x00000011
	timerID              = 1
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	setLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	setWindowDisplayAffinity   = user32.NewProc("SetWindowDisplayAffinity")

	backgroundColor win.COLORREF = rgb(255, 0, 255)
	cardColor       win.COLORREF = rgb(19, 24, 33)
	primaryColor    win.COLORREF = rgb(242, 246, 252)
	secondaryColor  win.COLORREF = rgb(160, 174, 192)
	dimColor        win.COLORREF = rgb(101, 115, 134)
	liveColor       win.COLORREF = rgb(255, 70, 76)
	doneColor       win.COLORREF = rgb(64, 215, 132)
	warningColor    win.COLORREF = rgb(255, 184, 72)

	activeWindow *overlayWindow
)

type config struct {
	EventAPIURL    string
	EventTokenFile string
	LogFile        string
	AllowCapture   bool
}

type overlayWindow struct {
	hwnd      win.HWND
	model     *actionosd.Model
	pulse     bool
	streamErr chan error
	closeOnce sync.Once
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
	after := uint64(0)
	if last > eventstream.DefaultReplayLimit {
		after = last - eventstream.DefaultReplayLimit
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
	logger.Info("action_osd_started", "event_api_url", cfg.EventAPIURL, "after_cursor", after, "capture_excluded", !cfg.AllowCapture)
	window.refresh()

	streamContext, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	go func() {
		err := client.Stream(streamContext, after, func(event eventstream.Event) error {
			if err := window.model.Apply(event); err != nil {
				return err
			}
			if event.Stream == actionrun.StreamName {
				win.PostMessage(window.hwnd, wmModelChanged, 0, 0)
			}
			return nil
		})
		if streamContext.Err() == nil {
			window.streamErr <- err
			win.PostMessage(window.hwnd, wmStreamFailed, 0, 0)
		}
	}()

	var message win.MSG
	for {
		status := win.GetMessage(&message, 0, 0, 0)
		if status == 0 {
			select {
			case streamErr := <-window.streamErr:
				return fmt.Errorf("consume event stream: %w", streamErr)
			default:
				logger.Info("action_osd_stopped")
				return nil
			}
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
	hwnd := win.CreateWindowEx(exStyle, className, windowName, win.WS_POPUP, x, y, windowWidth, windowHeight, 0, 0, instance, nil)
	if hwnd == 0 {
		win.UnregisterClass(className)
		win.DeleteObject(win.HGDIOBJ(backgroundBrush))
		return nil, errorsFromLast("create OSD window")
	}
	layered, _, _ := setLayeredWindowAttributes.Call(uintptr(hwnd), uintptr(backgroundColor), 235, lwaColorKey|lwaAlpha)
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
	window := &overlayWindow{hwnd: hwnd, model: model, pulse: true, streamErr: make(chan error, 1)}
	activeWindow = window
	win.SetTimer(hwnd, timerID, 500, 0)
	return window, nil
}

func (w *overlayWindow) destroy() {
	w.closeOnce.Do(func() {
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
			activeWindow.paint()
		}
		return 0
	case win.WM_TIMER:
		if activeWindow != nil {
			activeWindow.pulse = !activeWindow.pulse
			activeWindow.refresh()
		}
		return 0
	case wmModelChanged:
		if activeWindow != nil {
			activeWindow.refresh()
		}
		return 0
	case wmStreamFailed:
		win.DestroyWindow(hwnd)
		return 0
	case wmNCHitTest:
		return uintptr(^uint32(0))
	case win.WM_CLOSE:
		win.DestroyWindow(hwnd)
		return 0
	case win.WM_DESTROY:
		if activeWindow != nil && activeWindow.hwnd == hwnd {
			activeWindow.hwnd = 0
		}
		win.PostQuitMessage(0)
		return 0
	default:
		return win.DefWindowProc(hwnd, message, wParam, lParam)
	}
}

func (w *overlayWindow) refresh() {
	snapshot := w.model.Snapshot(time.Now().UTC())
	if !snapshot.Visible {
		win.ShowWindow(w.hwnd, win.SW_HIDE)
		return
	}
	x, y := overlayPosition(win.GetForegroundWindow())
	win.SetWindowPos(w.hwnd, win.HWND_TOPMOST, x, y, windowWidth, windowHeight, win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	win.InvalidateRect(w.hwnd, nil, true)
}

func (w *overlayWindow) paint() {
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
	cardBrush := solidBrush(cardColor)
	statusColor := colorForStatus(snapshot.Status)
	statusBrush := solidBrush(statusColor)
	if cardBrush == 0 || statusBrush == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(cardBrush))
	defer win.DeleteObject(win.HGDIOBJ(statusBrush))
	previousBrush := win.SelectObject(hdc, win.HGDIOBJ(cardBrush))
	win.RoundRect(hdc, 0, 0, windowWidth, windowHeight, 22, 22)
	if snapshot.Status != actionosd.StatusLive || w.pulse {
		win.SelectObject(hdc, win.HGDIOBJ(statusBrush))
		win.Ellipse(hdc, 22, 22, 38, 38)
	}
	win.SelectObject(hdc, previousBrush)
	win.SetBkMode(hdc, win.TRANSPARENT)
	drawText(hdc, snapshot.Status, 48, 15, 150, 45, 18, win.FW_BOLD, statusColor, win.DT_LEFT)
	drawText(hdc, elapsed(snapshot, time.Now().UTC()), 400, 15, 494, 45, 17, win.FW_NORMAL, secondaryColor, win.DT_RIGHT)
	drawText(hdc, snapshot.ActionID, 22, 51, 494, 84, 23, win.FW_BOLD, primaryColor, win.DT_LEFT|win.DT_END_ELLIPSIS)
	for index, activity := range snapshot.Activities {
		y := int32(99 + index*34)
		color := dimColor
		if index == len(snapshot.Activities)-1 {
			color = activityColor(activity.Level)
		} else if index == len(snapshot.Activities)-2 {
			color = secondaryColor
		}
		timestamp := activity.ObservedAt.Local().Format("15:04:05")
		drawText(hdc, timestamp, 22, y, 96, y+27, 14, win.FW_NORMAL, dimColor, win.DT_LEFT)
		drawText(hdc, activity.Message, 104, y, 494, y+27, 16, win.FW_NORMAL, color, win.DT_LEFT|win.DT_END_ELLIPSIS)
	}
}

func overlayPosition(foreground win.HWND) (int32, int32) {
	monitor := win.MonitorFromWindow(foreground, win.MONITOR_DEFAULTTOPRIMARY)
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if monitor != 0 && win.GetMonitorInfo(monitor, &info) {
		return info.RcWork.Right - windowWidth - margin, info.RcWork.Top + margin
	}
	return 1920 - windowWidth - margin, margin
}

func elapsed(snapshot actionosd.Snapshot, now time.Time) string {
	end := now
	if snapshot.Status != actionosd.StatusLive && !snapshot.TerminalAt.IsZero() {
		end = snapshot.TerminalAt
	}
	duration := end.Sub(snapshot.StartedAt)
	if duration < 0 {
		duration = 0
	}
	total := int(duration.Seconds())
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
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
		LfQuality: win.CLEARTYPE_QUALITY, LfFaceName: utf16Face("Segoe UI"),
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
