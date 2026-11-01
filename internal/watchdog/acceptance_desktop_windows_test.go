//go:build windows

package watchdog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	liveUser32              = windows.NewLazySystemDLL("user32.dll")
	liveEnumDisplaySettings = liveUser32.NewProc("EnumDisplaySettingsW")
	liveGetConsoleWindow    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
)

func liveRequireWindowless(t *testing.T) []byte {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := liveCheckGUIBinary(binary); err != nil {
		t.Fatal(err)
	}
	if console, _, _ := liveGetConsoleWindow.Call(); console != 0 {
		t.Fatalf("acceptance process has a console window: %#x", console)
	}
	return binary
}

type liveDesktopWindow struct {
	Handle     string `json:"handle"`
	PID        uint32 `json:"pid"`
	Class      string `json:"class"`
	Executable string `json:"executable,omitempty"`
	Owned      bool   `json:"owned"`
}

type liveDesktopMode struct {
	Width      uint32 `json:"width"`
	Height     uint32 `json:"height"`
	BitsPerPel uint32 `json:"bitsPerPel"`
	Frequency  uint32 `json:"frequencyHz"`
}

// All desktop observations are read-only. The hidden, non-activating top-level
// window receives WM_DISPLAYCHANGE; a message-only window would miss broadcasts.
// WinEvent hooks retain brief foreground/show events that polling could miss.
type liveDesktopTrace struct {
	mu        sync.Mutex
	file      *os.File
	encoder   *json.Encoder
	err       error
	interrupt context.CancelCauseFunc
	started   time.Time
	root      string
	done      chan struct{}
	window    win.HWND

	// Accessed only by the locked message-loop thread until done is closed.
	foreground        liveDesktopWindow
	mode              liveDesktopMode
	foregroundChanges int
	displayChanges    int
	consoleShows      int
	ownedWindowShows  int
}

func liveWatchDesktop(t *testing.T, root string, interrupt context.CancelCauseFunc) *liveDesktopTrace {
	t.Helper()
	var file *os.File
	var err error
	if path := os.Getenv("WINDOWSAGENT_WATCHDOG_DESKTOP_LOG"); path != "" {
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	} else {
		file, err = os.CreateTemp("", "windowsagent-watchdog-desktop-*.jsonl")
	}
	if err != nil {
		t.Fatalf("create private desktop trace: %v", err)
	}
	d := &liveDesktopTrace{file: file, encoder: json.NewEncoder(file), interrupt: interrupt, started: time.Now(), root: root, done: make(chan struct{})}
	t.Logf("desktopTrace=%s", file.Name())
	ready := make(chan error, 1)
	go d.run(ready)
	if err := <-ready; err != nil {
		<-d.done
		_ = file.Close()
		t.Fatalf("start desktop observation: %v trace=%s", err, file.Name())
	}
	// Registered before the fixture Task cleanups, so observation covers their
	// PowerShell children and every Task stop/unregister operation as well.
	t.Cleanup(func() {
		d.record("phase", map[string]string{"name": "cleanup_complete"})
		if win.PostMessage(d.window, win.WM_APP+1, 0, 0) == 0 {
			_ = file.Close()
			t.Error("stop desktop observer: PostMessage failed")
			return
		}
		<-d.done
		if err := file.Close(); err != nil {
			t.Error(err)
		}
		if d.err != nil {
			t.Errorf("desktop acceptance failed: %v trace=%s", d.err, file.Name())
		}
		t.Logf("desktop foregroundChanges=%d displayChanges=%d consoleShows=%d ownedWindowShows=%d trace=%s", d.foregroundChanges, d.displayChanges, d.consoleShows, d.ownedWindowShows, file.Name())
	})
	return d
}

func (d *liveDesktopTrace) record(event string, data any) {
	d.mu.Lock()
	err := d.encoder.Encode(struct {
		ObservedAt time.Time `json:"observedAt"`
		ElapsedMS  int64     `json:"elapsedMs"`
		Event      string    `json:"event"`
		Data       any       `json:"data"`
	}{time.Now().UTC(), time.Since(d.started).Milliseconds(), event, data})
	if err == nil {
		err = d.file.Sync()
	}
	d.mu.Unlock()
	if err != nil {
		d.fail(fmt.Errorf("write desktop trace: %w", err))
	}
}

func (d *liveDesktopTrace) fail(err error) {
	d.mu.Lock()
	if d.err == nil {
		d.err = err
	}
	d.mu.Unlock()
	d.interrupt(err)
}

func (d *liveDesktopTrace) observeForeground(window liveDesktopWindow, nativeTime uint32, source string) {
	d.record("foreground", map[string]any{"source": source, "nativeTimeMs": nativeTime, "previous": d.foreground, "current": window})
	if window.Handle != d.foreground.Handle {
		d.foregroundChanges++
		d.fail(fmt.Errorf("foreground changed from %s to %s", d.foreground.Handle, window.Handle))
	}
	d.foreground = window
}

func (d *liveDesktopTrace) observeShow(window liveDesktopWindow, nativeTime uint32) {
	d.record("window_show", map[string]any{"nativeTimeMs": nativeTime, "window": window})
	if window.Class == "ConsoleWindowClass" {
		d.consoleShows++
		d.fail(fmt.Errorf("console window shown: %s", window.Handle))
	}
	if window.Owned {
		d.ownedWindowShows++
		d.fail(fmt.Errorf("acceptance process showed a window: %s", window.Handle))
	}
}

func (d *liveDesktopTrace) observeDisplay(mode liveDesktopMode, bits, width, height uint32, source string) {
	d.record("display_change", map[string]any{"source": source, "previous": d.mode, "current": mode, "announcedBitsPerPel": bits, "announcedWidth": width, "announcedHeight": height})
	d.displayChanges++
	d.mode = mode
	d.fail(fmt.Errorf("display mode changed (%s): %dx%d at %d Hz", source, mode.Width, mode.Height, mode.Frequency))
}

func (d *liveDesktopTrace) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.done)
	class := windows.StringToUTF16Ptr(fmt.Sprintf("WindowsAgentWatchdogAcceptance-%d", os.Getpid()))
	instance := win.GetModuleHandle(nil)
	wndProc := windows.NewCallback(func(hwnd win.HWND, message uint32, wparam, lparam uintptr) uintptr {
		switch message {
		case win.WM_DISPLAYCHANGE:
			mode, err := liveReadDisplayMode()
			if err != nil {
				d.fail(err)
			} else {
				d.observeDisplay(mode, uint32(wparam), uint32(lparam&0xffff), uint32((lparam>>16)&0xffff), "WM_DISPLAYCHANGE")
			}
		case win.WM_APP + 1:
			d.observeForeground(d.readWindow(win.GetForegroundWindow()), 0, "final")
			mode, err := liveReadDisplayMode()
			if err != nil {
				d.fail(err)
			} else if mode != d.mode {
				d.observeDisplay(mode, mode.BitsPerPel, mode.Width, mode.Height, "final")
			}
			d.record("summary", map[string]any{"foreground": d.foreground, "primaryDisplay": mode, "foregroundChanges": d.foregroundChanges, "displayChanges": d.displayChanges, "consoleShows": d.consoleShows, "ownedWindowShows": d.ownedWindowShows})
			win.PostQuitMessage(0)
			return 0
		}
		return win.DefWindowProc(hwnd, message, wparam, lparam)
	})
	wc := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), HInstance: instance, LpszClassName: class, LpfnWndProc: wndProc}
	if win.RegisterClassEx(&wc) == 0 {
		ready <- fmt.Errorf("register desktop observer window class: %v", windows.GetLastError())
		return
	}
	defer win.UnregisterClass(class)
	// Never add WS_VISIBLE, ShowWindow, or an activating operation here.
	d.window = win.CreateWindowEx(win.WS_EX_NOACTIVATE|win.WS_EX_TOOLWINDOW, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, instance, nil)
	if d.window == 0 {
		ready <- fmt.Errorf("create hidden desktop observer window: %v", windows.GetLastError())
		return
	}
	defer win.DestroyWindow(d.window)
	callback := func(_ win.HWINEVENTHOOK, event uint32, hwnd win.HWND, object, child int32, _ uint32, nativeTime uint32) uintptr {
		if event == win.EVENT_SYSTEM_FOREGROUND {
			d.observeForeground(d.readWindow(hwnd), nativeTime, "WinEvent")
		} else if object == 0 && child == 0 && hwnd != 0 {
			root := win.GetAncestor(hwnd, win.GA_ROOT)
			if root == hwnd || root == 0 {
				d.observeShow(d.readWindow(hwnd), nativeTime)
			}
		}
		return 0
	}
	for _, event := range []uint32{win.EVENT_SYSTEM_FOREGROUND, win.EVENT_OBJECT_SHOW} {
		hook, err := win.SetWinEventHook(event, event, 0, callback, 0, 0, win.WINEVENT_OUTOFCONTEXT)
		if err != nil {
			ready <- fmt.Errorf("install desktop WinEvent hook: %w", err)
			return
		}
		defer func() {
			if !win.UnhookWinEvent(hook) {
				d.fail(fmt.Errorf("remove desktop WinEvent hook"))
			}
		}()
	}
	mode, err := liveReadDisplayMode()
	if err != nil {
		ready <- err
		return
	}
	d.mode = mode
	d.foreground = d.readWindow(win.GetForegroundWindow())
	d.record("baseline", map[string]any{"foreground": d.foreground, "primaryDisplay": mode, "observerVisible": win.IsWindowVisible(d.window), "consoleWindow": 0})
	ready <- nil
	var message win.MSG
	for {
		result := win.GetMessage(&message, 0, 0, 0)
		if result == 0 {
			return
		}
		if result == -1 {
			d.fail(fmt.Errorf("desktop observer GetMessage: %v", windows.GetLastError()))
			return
		}
		win.TranslateMessage(&message)
		win.DispatchMessage(&message)
	}
}

func (d *liveDesktopTrace) readWindow(hwnd win.HWND) liveDesktopWindow {
	info := liveDesktopWindow{Handle: fmt.Sprintf("%#x", uintptr(hwnd))}
	if hwnd == 0 {
		return info
	}
	win.GetWindowThreadProcessId(hwnd, &info.PID)
	var class [256]uint16
	_, _ = win.GetClassName(hwnd, &class[0], len(class))
	info.Class = windows.UTF16ToString(class[:])
	info.Owned = info.PID == uint32(os.Getpid())
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, info.PID)
	if err == nil {
		defer windows.CloseHandle(process)
		path := make([]uint16, 32768)
		size := uint32(len(path))
		if windows.QueryFullProcessImageName(process, 0, &path[0], &size) == nil {
			name := windows.UTF16ToString(path[:size])
			info.Executable = filepath.Base(name)
			info.Owned = info.Owned || strings.HasPrefix(strings.ToLower(name), strings.ToLower(d.root)+string(filepath.Separator))
		}
	}
	return info
}

func liveReadDisplayMode() (liveDesktopMode, error) {
	var mode win.DEVMODE
	mode.DmSize = uint16(unsafe.Sizeof(mode))
	result, _, _ := liveEnumDisplaySettings.Call(0, uintptr(^uint32(0)), uintptr(unsafe.Pointer(&mode)))
	if result == 0 {
		return liveDesktopMode{}, fmt.Errorf("EnumDisplaySettingsW(ENUM_CURRENT_SETTINGS) failed")
	}
	return liveDesktopMode{mode.DmPelsWidth, mode.DmPelsHeight, mode.DmBitsPerPel, mode.DmDisplayFrequency}, nil
}

func TestWatchdogAcceptanceDesktopInterruptions(t *testing.T) {
	for _, scenario := range []string{"foreground_unchanged", "foreground_roundtrip", "console_show", "owned_window_show", "display_roundtrip", "trace_write_failure"} {
		t.Run(scenario, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "trace-*.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			d := &liveDesktopTrace{file: file, encoder: json.NewEncoder(file), interrupt: cancel, started: time.Now(), foreground: liveDesktopWindow{Handle: "0x1"}, mode: liveDesktopMode{Width: 3840, Height: 2160}}
			switch scenario {
			case "foreground_unchanged":
				d.observeForeground(liveDesktopWindow{Handle: "0x1"}, 100, "WinEvent")
			case "foreground_roundtrip":
				d.observeForeground(liveDesktopWindow{Handle: "0x2"}, 100, "WinEvent")
				d.observeForeground(liveDesktopWindow{Handle: "0x1"}, 101, "WinEvent")
			case "console_show":
				d.observeShow(liveDesktopWindow{Handle: "0x2", Class: "ConsoleWindowClass"}, 100)
			case "owned_window_show":
				d.observeShow(liveDesktopWindow{Handle: "0x2", Owned: true}, 100)
			case "display_roundtrip":
				d.observeDisplay(liveDesktopMode{Width: 2560, Height: 1440}, 32, 2560, 1440, "WM_DISPLAYCHANGE")
				d.observeDisplay(liveDesktopMode{Width: 3840, Height: 2160}, 32, 3840, 2160, "WM_DISPLAYCHANGE")
			case "trace_write_failure":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				d.record("phase", "unwritable")
			}
			if scenario == "foreground_unchanged" {
				if context.Cause(ctx) != nil || d.err != nil {
					t.Fatalf("unchanged foreground interrupted acceptance: %v", d.err)
				}
			} else if context.Cause(ctx) == nil || d.err == nil {
				t.Fatal("desktop interruption did not stop acceptance")
			}
			if scenario == "trace_write_failure" {
				return
			}
			data, err := os.ReadFile(file.Name())
			if err != nil || len(data) == 0 {
				t.Fatalf("interruption evidence missing: %v", err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if strings.HasSuffix(scenario, "roundtrip") && len(lines) != 2 {
				t.Fatalf("transient roundtrip evidence missing: %s", data)
			}
			for _, line := range lines {
				if !json.Valid([]byte(line)) {
					t.Fatalf("invalid desktop evidence: %s", line)
				}
			}
		})
	}
}
