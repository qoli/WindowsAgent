//go:build windows && amd64

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/qoli/WindowsAgent/internal/assistgui"
	"github.com/qoli/WindowsAgent/internal/assistinstall"
	"github.com/qoli/WindowsAgent/internal/releasecatalog"
	"github.com/qoli/WindowsAgent/internal/releasedownload"
	"golang.org/x/sys/windows"
)

var version = "dev"

const (
	refreshButtonID   = 1001
	tailscaleToggleID = 1002
	installButtonID   = 1003
	updateButtonID    = 1004
	repairButtonID    = 1005
	uninstallButtonID = 1006
	watchdogToggleID  = 1007
	refreshMessage    = win.WM_APP + 1
	latestCatalogURL  = "https://github.com/qoli/WindowsAgent/releases/latest/download/windowsagent-release.json"
)

var (
	windowHandle    win.HWND
	statusLabel     win.HWND
	accessLabel     win.HWND
	tailscaleLabel  win.HWND
	refreshButton   win.HWND
	installButton   win.HWND
	updateButton    win.HWND
	repairButton    win.HWND
	uninstallButton win.HWND
	watchdogToggle  win.HWND
	tailscaleToggle win.HWND
	authKeyEdit     win.HWND
	textMu          sync.Mutex
	pendingText     windowText
	adapterMu       sync.Mutex
	adapterCommand  *exec.Cmd
	adapterDone     chan struct{}
)

type windowText struct {
	status, access, tailscale string
}

func main() {
	if err := runMain(); err != nil {
		if os.Getenv("WINDOWSAGENT_ASSIST_HEADLESS") == "1" {
			_, _ = fmt.Fprintln(os.Stderr, err)
		} else {
			title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
			message, _ := windows.UTF16PtrFromString(err.Error())
			win.MessageBox(0, message, title, win.MB_OK|win.MB_ICONERROR)
		}
		os.Exit(1)
	}
}

func runMain() error {
	if len(os.Args) > 1 && os.Args[1] == "--apply-release" {
		if len(os.Args) != 8 {
			return fmt.Errorf("--apply-release requires operation, stage, catalog, data directory, Watchdog startup setting, and parent process arguments")
		}
		operation := assistinstall.Operation(os.Args[2])
		watchdogStartAtLogon, err := strconv.ParseBool(os.Args[6])
		if err != nil {
			return fmt.Errorf("invalid Watchdog startup setting %q", os.Args[6])
		}
		parentPID, err := parsePID(os.Args[7])
		if err != nil {
			return err
		}
		if err := waitForProcessExit(parentPID, 45*time.Second); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := assistinstall.Apply(ctx, assistinstall.Request{Operation: operation, StageDir: os.Args[3], CatalogPath: os.Args[4], DataDir: os.Args[5], WatchdogStartAtLogon: watchdogStartAtLogon}); err != nil {
			return err
		}
		if os.Getenv("WINDOWSAGENT_ASSIST_HEADLESS") != "1" {
			title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
			message, _ := windows.UTF16PtrFromString("WindowsAgent " + string(operation) + " completed and passed local health checks.")
			win.MessageBox(0, message, title, win.MB_OK|win.MB_ICONINFORMATION)
		}
		return nil
	}
	if len(os.Args) > 1 && os.Args[1] == "--uninstall" {
		if len(os.Args) != 4 {
			return fmt.Errorf("--uninstall requires data directory and parent process arguments")
		}
		parentPID, err := parsePID(os.Args[3])
		if err != nil {
			return err
		}
		if err := waitForProcessExit(parentPID, 45*time.Second); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := assistinstall.Uninstall(ctx, os.Args[2]); err != nil {
			return err
		}
		if os.Getenv("WINDOWSAGENT_ASSIST_HEADLESS") != "1" {
			title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
			message, _ := windows.UTF16PtrFromString("WindowsAgent was uninstalled. User data was preserved.")
			win.MessageBox(0, message, title, win.MB_OK|win.MB_ICONINFORMATION)
		}
		return nil
	}
	if len(os.Args) > 1 && os.Args[1] == "--configure-watchdog" {
		if len(os.Args) != 4 {
			return fmt.Errorf("--configure-watchdog requires startup setting and data directory arguments")
		}
		startAtLogon, err := strconv.ParseBool(os.Args[2])
		if err != nil {
			return fmt.Errorf("invalid Watchdog startup setting %q", os.Args[2])
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := assistinstall.ConfigureWatchdog(ctx, os.Args[3], startAtLogon); err != nil {
			return err
		}
		if os.Getenv("WINDOWSAGENT_ASSIST_HEADLESS") != "1" {
			title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
			message, _ := windows.UTF16PtrFromString("Watchdog start-at-sign-in setting was updated.")
			win.MessageBox(0, message, title, win.MB_OK|win.MB_ICONINFORMATION)
		}
		return nil
	}
	if len(os.Args) != 1 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(os.Args[1:], " "))
	}
	return run()
}

func run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mutexName, _ := windows.UTF16PtrFromString(`Local\WindowsAgent.AssistGUI`)
	mutex, mutexErr := windows.CreateMutex(nil, false, mutexName)
	if mutex == 0 {
		return fmt.Errorf("create AssistGUI single-instance mutex: %w", mutexErr)
	}
	defer windows.CloseHandle(mutex)
	if errors.Is(mutexErr, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("WindowsAgent Assist is already running for this user")
	}
	instance := win.GetModuleHandle(nil)
	if instance == 0 {
		return windows.GetLastError()
	}
	className, _ := windows.UTF16PtrFromString("WindowsAgentAssistGUI")
	title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
	wndClass := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   windows.NewCallback(windowProc),
		HInstance:     instance,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
		HbrBackground: win.HBRUSH(win.COLOR_WINDOW + 1),
		LpszClassName: className,
	}
	if win.RegisterClassEx(&wndClass) == 0 {
		return fmt.Errorf("register Assist GUI window class: %w", windows.GetLastError())
	}
	windowHandle = win.CreateWindowEx(0, className, title, win.WS_OVERLAPPEDWINDOW,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT, 680, 620, 0, 0, instance, nil)
	if windowHandle == 0 {
		return fmt.Errorf("create Assist GUI window: %w", windows.GetLastError())
	}
	win.ShowWindow(windowHandle, win.SW_SHOWDEFAULT)
	win.UpdateWindow(windowHandle)
	refreshAsync()
	var message win.MSG
	for {
		status := win.GetMessage(&message, 0, 0, 0)
		if status == 0 {
			return nil
		}
		if status == -1 {
			return fmt.Errorf("read Assist GUI message: %w", windows.GetLastError())
		}
		win.TranslateMessage(&message)
		win.DispatchMessage(&message)
	}
}

func windowProc(hwnd win.HWND, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case win.WM_CREATE:
		instance := win.GetModuleHandle(nil)
		statusLabel = createControl("STATIC", "Checking WindowsAgent...", win.WS_CHILD|win.WS_VISIBLE|win.SS_LEFT, 24, 24, 610, 54, hwnd, 0, instance)
		accessLabel = createControl("STATIC", "LAN endpoints: checking...", win.WS_CHILD|win.WS_VISIBLE|win.SS_LEFT, 24, 92, 610, 150, hwnd, 0, instance)
		tailscaleLabel = createControl("STATIC", "Tailscale: checking...", win.WS_CHILD|win.WS_VISIBLE|win.SS_LEFT, 24, 252, 610, 80, hwnd, 0, instance)
		createControl("STATIC", "One-off ephemeral auth key:", win.WS_CHILD|win.WS_VISIBLE|win.SS_LEFT, 24, 345, 220, 24, hwnd, 0, instance)
		authKeyEdit = createControl("EDIT", "", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.WS_BORDER|win.ES_PASSWORD|win.ES_AUTOHSCROLL, 244, 340, 390, 28, hwnd, 0, instance)
		tailscaleToggle = createControl("BUTTON", "Enable Tailscale access", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_AUTOCHECKBOX, 24, 386, 250, 32, hwnd, win.HMENU(tailscaleToggleID), instance)
		watchdogToggle = createControl("BUTTON", "Start Watchdog at sign-in", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_AUTOCHECKBOX, 344, 386, 288, 32, hwnd, win.HMENU(watchdogToggleID), instance)
		win.SendMessage(watchdogToggle, win.BM_SETCHECK, win.BST_CHECKED, 0)
		installButton = createControl("BUTTON", "Install", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON, 24, 440, 140, 34, hwnd, win.HMENU(installButtonID), instance)
		updateButton = createControl("BUTTON", "Update", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON, 180, 440, 140, 34, hwnd, win.HMENU(updateButtonID), instance)
		repairButton = createControl("BUTTON", "Repair", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON, 336, 440, 140, 34, hwnd, win.HMENU(repairButtonID), instance)
		uninstallButton = createControl("BUTTON", "Uninstall", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON, 492, 440, 140, 34, hwnd, win.HMENU(uninstallButtonID), instance)
		refreshButton = createControl("BUTTON", "Refresh", win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON, 522, 496, 112, 34, hwnd, win.HMENU(refreshButtonID), instance)
		if statusLabel == 0 || accessLabel == 0 || tailscaleLabel == 0 || authKeyEdit == 0 || tailscaleToggle == 0 || watchdogToggle == 0 || installButton == 0 || updateButton == 0 || repairButton == 0 || uninstallButton == 0 || refreshButton == 0 {
			return ^uintptr(0)
		}
		return 0
	case win.WM_COMMAND:
		switch uint16(wParam & 0xffff) {
		case refreshButtonID:
			refreshAsync()
			return 0
		case tailscaleToggleID:
			if win.SendMessage(tailscaleToggle, win.BM_GETCHECK, 0, 0) == win.BST_CHECKED {
				authKey, err := readSecret(authKeyEdit)
				if err == nil {
					setText(authKeyEdit, "")
					err = startTailscaleAdapter(authKey)
				}
				if err != nil {
					win.SendMessage(tailscaleToggle, win.BM_SETCHECK, win.BST_UNCHECKED, 0)
					showError(err)
				}
			} else if err := stopTailscaleAdapter(); err != nil {
				showError(err)
			}
			refreshAsync()
			return 0
		case installButtonID:
			installAsync(assistinstall.OperationInstall)
			return 0
		case updateButtonID:
			installAsync(assistinstall.OperationUpdate)
			return 0
		case repairButtonID:
			installAsync(assistinstall.OperationRepair)
			return 0
		case uninstallButtonID:
			uninstallAsync()
			return 0
		case watchdogToggleID:
			configureWatchdogAsync(win.SendMessage(watchdogToggle, win.BM_GETCHECK, 0, 0) == win.BST_CHECKED)
			return 0
		}
	case refreshMessage:
		textMu.Lock()
		text := pendingText
		textMu.Unlock()
		setText(statusLabel, text.status)
		setText(accessLabel, text.access)
		setText(tailscaleLabel, text.tailscale)
		return 0
	case win.WM_CLOSE:
		win.DestroyWindow(hwnd)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, message, wParam, lParam)
}

func installAsync(operation assistinstall.Operation) {
	watchdogStartAtLogon := win.SendMessage(watchdogToggle, win.BM_GETCHECK, 0, 0) == win.BST_CHECKED
	setOperationButtonsEnabled(false)
	setText(statusLabel, "WindowsAgent: preparing "+string(operation)+"...")
	go func() {
		handedOff := false
		defer func() {
			if !handedOff {
				setOperationButtonsEnabled(true)
			}
		}()
		dataDir, err := assistDataDir()
		if err != nil {
			showError(err)
			return
		}
		if status := assistgui.LoadTailscaleStatus(dataDir); status.Enabled {
			if err := stopTailscaleAdapter(); err != nil {
				showError(fmt.Errorf("stop TailscaleAdapter before %s: %w", operation, err))
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		downloader := releasedownload.Client{HTTP: releasedownload.NewHTTP1Client()}
		catalog, base, err := downloader.FetchCatalog(ctx, latestCatalogURL)
		if err != nil {
			showError(err)
			refreshAsync()
			return
		}
		stage := filepath.Join(dataDir, "release-staging", catalog.Version+"-"+fmt.Sprint(time.Now().UTC().UnixNano()))
		if err := downloader.Stage(ctx, base, catalog, stage, releasecatalog.InstallArtifact); err != nil {
			showError(err)
			refreshAsync()
			return
		}
		catalogPath := filepath.Join(stage, "windowsagent-release.json")
		if err := writeReleaseMetadata(catalogPath, filepath.Join(stage, "SHA256SUMS"), catalog); err != nil {
			showError(err)
			refreshAsync()
			return
		}
		helper := filepath.Join(stage, "windows-assist-gui.exe")
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(helper)
		parameters, _ := windows.UTF16PtrFromString(strings.Join([]string{
			"--apply-release", string(operation), syscall.EscapeArg(stage), syscall.EscapeArg(catalogPath), syscall.EscapeArg(dataDir), strconv.FormatBool(watchdogStartAtLogon), fmt.Sprint(os.Getpid()),
		}, " "))
		cwd, _ := windows.UTF16PtrFromString(stage)
		if err := windows.ShellExecute(windows.Handle(windowHandle), verb, file, parameters, cwd, win.SW_SHOWNORMAL); err != nil {
			showError(fmt.Errorf("start elevated WindowsAgent installer: %w", err))
			return
		}
		handedOff = true
		setText(statusLabel, "WindowsAgent: elevated "+string(operation)+" started; approve the Windows prompt")
		win.PostMessage(windowHandle, win.WM_CLOSE, 0, 0)
	}()
}

func uninstallAsync() {
	setOperationButtonsEnabled(false)
	setText(statusLabel, "WindowsAgent: preparing uninstall...")
	go func() {
		handedOff := false
		defer func() {
			if !handedOff {
				setOperationButtonsEnabled(true)
			}
		}()
		dataDir, err := assistDataDir()
		if err != nil {
			showError(err)
			return
		}
		executable, err := os.Executable()
		if err != nil {
			showError(fmt.Errorf("resolve AssistGUI executable: %w", err))
			return
		}
		stage := filepath.Join(dataDir, "release-staging", "uninstall-"+fmt.Sprint(time.Now().UTC().UnixNano()))
		if err := os.MkdirAll(stage, 0o700); err != nil {
			showError(fmt.Errorf("create uninstall staging directory: %w", err))
			return
		}
		helper := filepath.Join(stage, "windows-assist-gui.exe")
		if err := copyVerified(executable, helper); err != nil {
			showError(err)
			return
		}
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(helper)
		parameters, _ := windows.UTF16PtrFromString(strings.Join([]string{
			"--uninstall", syscall.EscapeArg(dataDir), fmt.Sprint(os.Getpid()),
		}, " "))
		cwd, _ := windows.UTF16PtrFromString(stage)
		if err := windows.ShellExecute(windows.Handle(windowHandle), verb, file, parameters, cwd, win.SW_SHOWNORMAL); err != nil {
			showError(fmt.Errorf("start elevated WindowsAgent uninstaller: %w", err))
			return
		}
		handedOff = true
		setText(statusLabel, "WindowsAgent: elevated uninstall started; approve the Windows prompt")
		win.PostMessage(windowHandle, win.WM_CLOSE, 0, 0)
	}()
}

func configureWatchdogAsync(startAtLogon bool) {
	dataDir, err := assistDataDir()
	if err != nil {
		showError(err)
		return
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bin", "windows-capture-agent.exe")); err != nil {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		showError(fmt.Errorf("resolve AssistGUI executable: %w", err))
		return
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(executable)
	parameters, _ := windows.UTF16PtrFromString(strings.Join([]string{
		"--configure-watchdog", strconv.FormatBool(startAtLogon), syscall.EscapeArg(dataDir),
	}, " "))
	cwd, _ := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err := windows.ShellExecute(windows.Handle(windowHandle), verb, file, parameters, cwd, win.SW_SHOWNORMAL); err != nil {
		showError(fmt.Errorf("start elevated Watchdog configurator: %w", err))
		return
	}
	setText(statusLabel, "Watchdog: elevated setting update started; approve the Windows prompt")
	go func() {
		time.Sleep(3 * time.Second)
		refreshAsync()
	}()
}

func copyVerified(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read AssistGUI for staging: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o700); err != nil {
		return fmt.Errorf("write staged AssistGUI: %w", err)
	}
	written, err := os.ReadFile(destination)
	if err != nil {
		return fmt.Errorf("read staged AssistGUI: %w", err)
	}
	if sha256.Sum256(written) != sha256.Sum256(data) {
		return fmt.Errorf("staged AssistGUI SHA-256 mismatch")
	}
	return nil
}

func setOperationButtonsEnabled(enabled bool) {
	for _, button := range []win.HWND{installButton, updateButton, repairButton, uninstallButton} {
		if button != 0 {
			win.EnableWindow(button, enabled)
		}
	}
}

func setOperationAvailability(installed bool) {
	win.EnableWindow(installButton, !installed)
	for _, button := range []win.HWND{updateButton, repairButton, uninstallButton, tailscaleToggle} {
		win.EnableWindow(button, installed)
	}
}

func parsePID(value string) (uint32, error) {
	var pid uint32
	if _, err := fmt.Sscan(value, &pid); err != nil || pid == 0 {
		return 0, fmt.Errorf("invalid parent process ID %q", value)
	}
	return pid, nil
}

func waitForProcessExit(pid uint32, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		return fmt.Errorf("open parent AssistGUI process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, uint32(timeout/time.Millisecond))
	if err != nil {
		return fmt.Errorf("wait for parent AssistGUI process %d: %w", pid, err)
	}
	if result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("parent AssistGUI process %d did not exit before installation", pid)
	}
	return nil
}

func writeReleaseMetadata(catalogPath, sumsPath string, catalog releasecatalog.Catalog) error {
	for path, write := range map[string]func(io.Writer) error{
		catalogPath: func(writer io.Writer) error { return releasecatalog.WriteJSON(writer, catalog) },
		sumsPath:    func(writer io.Writer) error { return releasecatalog.WriteSHA256Sums(writer, catalog) },
	} {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		writeErr := write(file)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func createControl(class, text string, style uint32, x, y, width, height int32, parent win.HWND, menu win.HMENU, instance win.HINSTANCE) win.HWND {
	className, _ := windows.UTF16PtrFromString(class)
	windowText, _ := windows.UTF16PtrFromString(text)
	return win.CreateWindowEx(0, className, windowText, style, x, y, width, height, parent, menu, instance, nil)
}

func setText(hwnd win.HWND, value string) {
	pointer, _ := windows.UTF16PtrFromString(value)
	win.SendMessage(hwnd, win.WM_SETTEXT, 0, uintptr(unsafe.Pointer(pointer)))
}

func readSecret(hwnd win.HWND) ([]byte, error) {
	length := int(win.SendMessage(hwnd, win.WM_GETTEXTLENGTH, 0, 0))
	if length == 0 {
		return nil, fmt.Errorf("a one-off ephemeral Tailscale auth key is required")
	}
	buffer := make([]uint16, length+1)
	defer func() {
		for index := range buffer {
			buffer[index] = 0
		}
	}()
	win.SendMessage(hwnd, win.WM_GETTEXT, uintptr(len(buffer)), uintptr(unsafe.Pointer(&buffer[0])))
	secret := make([]byte, 0, length)
	for _, value := range buffer[:length] {
		if value > 0x7f {
			zero(secret)
			return nil, fmt.Errorf("Tailscale auth key must contain ASCII characters only")
		}
		if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
			zero(secret)
			return nil, fmt.Errorf("Tailscale auth key must not contain whitespace")
		}
		secret = append(secret, byte(value))
	}
	return secret, nil
}

func refreshAsync() {
	setText(statusLabel, "Checking WindowsAgent...")
	go func() {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			publish(windowText{status: "WindowsAgent: unavailable", access: "LAN endpoints: unavailable", tailscale: "Error: LOCALAPPDATA is required"})
			return
		}
		dataDir := filepath.Join(localAppData, "gameGuide", "windows-capture-agent")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		snapshot, err := (assistgui.Inspector{DataDir: dataDir, AgentURL: "http://127.0.0.1:8787/healthz", Port: "8787"}).Snapshot(ctx)
		if err != nil {
			publish(windowText{status: "WindowsAgent: inspection failed", access: "LAN endpoints: unavailable", tailscale: "Error: " + err.Error()})
			return
		}
		if snapshot.Tailscale.State == assistgui.TailscaleOnline || snapshot.Tailscale.State == assistgui.TailscaleStarting || snapshot.Tailscale.State == assistgui.TailscaleStopping {
			if err := verifyAdapterProcess(snapshot.Tailscale); err != nil {
				snapshot.Tailscale.State = assistgui.TailscaleFailed
				snapshot.Tailscale.Error = "stale adapter status: " + err.Error()
			}
		}
		status := "WindowsAgent: Not installed"
		if snapshot.Installed {
			status = "WindowsAgent: Installed\r\nCapture Agent: Stopped"
			if snapshot.InstalledVersion != "" {
				status = "WindowsAgent: Installed (" + snapshot.InstalledVersion + ")\r\nCapture Agent: Stopped"
			}
		}
		if snapshot.AgentHealthy {
			status = "WindowsAgent: Installed\r\nCapture Agent: Running"
			if snapshot.AgentVersion != "" {
				status += " (" + snapshot.AgentVersion + ")"
			}
		} else if snapshot.AgentHealthError != "" {
			status += "\r\n" + snapshot.AgentHealthError
		}
		watchdog, watchdogErr := inspectWatchdog()
		if watchdogErr != nil {
			status += "\r\nWatchdog: inspection failed: " + watchdogErr.Error()
		} else if watchdog.Installed {
			state := "Stopped"
			if watchdog.Running {
				state = "Running"
			}
			start := "No"
			if watchdog.StartAtLogon {
				start = "Yes"
			}
			status += "\r\nWatchdog: " + state + "; starts at sign-in: " + start
			win.SendMessage(watchdogToggle, win.BM_SETCHECK, mapCheck(watchdog.StartAtLogon), 0)
		}
		var endpoints []string
		for _, endpoint := range snapshot.LANEndpoints {
			endpoints = append(endpoints, endpoint.Interface+": "+endpoint.URL)
		}
		access := "LAN endpoints: none detected"
		if len(endpoints) != 0 {
			access = "LAN endpoints:\r\n" + strings.Join(endpoints, "\r\n")
		}
		tailscale := "Tailscale: disabled (default)"
		if snapshot.Tailscale.Enabled {
			win.SendMessage(tailscaleToggle, win.BM_SETCHECK, win.BST_CHECKED, 0)
			tailscale = "Tailscale: " + snapshot.Tailscale.State
			if snapshot.Tailscale.IPv4 != "" {
				tailscale += "\r\nIPv4: " + snapshot.Tailscale.IPv4
			}
			if snapshot.Tailscale.IPv6 != "" {
				tailscale += "\r\nIPv6: " + snapshot.Tailscale.IPv6
			}
			if snapshot.Tailscale.Error != "" {
				tailscale += "\r\n" + snapshot.Tailscale.Error
			}
		} else {
			win.SendMessage(tailscaleToggle, win.BM_SETCHECK, win.BST_UNCHECKED, 0)
		}
		setOperationAvailability(snapshot.Installed)
		publish(windowText{status: status, access: access, tailscale: tailscale})
	}()
}

type watchdogFacts struct {
	Installed    bool `json:"installed"`
	Running      bool `json:"running"`
	StartAtLogon bool `json:"startAtLogon"`
}

func inspectWatchdog() (watchdogFacts, error) {
	const script = `$task = Get-ScheduledTask -TaskName 'gameGuide Windows Watchdog' -ErrorAction SilentlyContinue; if (-not $task) { @{installed=$false;running=$false;startAtLogon=$false} | ConvertTo-Json -Compress; exit 0 }; if ($task.Description -cne 'gameGuide external process watchdog; no automatic self-recovery') { throw 'Watchdog Scheduled Task ownership mismatch' }; @{installed=$true;running=($task.State.ToString() -ceq 'Running');startAtLogon=(@($task.Triggers | Where-Object { $_.CimClass.CimClassName -ceq 'MSFT_TaskLogonTrigger' }).Count -gt 0)} | ConvertTo-Json -Compress`
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return watchdogFacts{}, fmt.Errorf("query Scheduled Task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var facts watchdogFacts
	if err := json.Unmarshal(output, &facts); err != nil {
		return watchdogFacts{}, fmt.Errorf("decode Scheduled Task state: %w", err)
	}
	return facts, nil
}

func mapCheck(checked bool) uintptr {
	if checked {
		return win.BST_CHECKED
	}
	return win.BST_UNCHECKED
}

func startTailscaleAdapter(authKey []byte) error {
	defer zero(authKey)
	if len(authKey) == 0 {
		return fmt.Errorf("a one-off ephemeral Tailscale auth key is required")
	}
	adapterMu.Lock()
	defer adapterMu.Unlock()
	if adapterCommand != nil && adapterCommand.Process != nil {
		return fmt.Errorf("TailscaleAdapter is already running")
	}
	dataDir, err := assistDataDir()
	if err != nil {
		return err
	}
	binDir := filepath.Join(dataDir, "bin")
	adapterPath := filepath.Join(binDir, "windows-tailscale-adapter.exe")
	if err := verifyAdjacentAdapter(binDir); err != nil {
		return err
	}
	existing := assistgui.LoadTailscaleStatus(dataDir)
	if existing.Enabled && verifyAdapterProcess(existing) == nil {
		return fmt.Errorf("TailscaleAdapter process %d is already active", existing.ProcessID)
	}
	adapterDir := filepath.Join(dataDir, "tailscale")
	if err := os.MkdirAll(adapterDir, 0o700); err != nil {
		return fmt.Errorf("create TailscaleAdapter data directory: %w", err)
	}
	stopFile := filepath.Join(adapterDir, "stop.request")
	if err := os.Remove(stopFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear stale TailscaleAdapter stop request: %w", err)
	}
	command := exec.Command(adapterPath,
		"--data-dir", adapterDir,
		"--status-file", filepath.Join(adapterDir, "status.json"),
		"--stop-file", stopFile,
		"--listen", ":8787",
		"--forward-to", "127.0.0.1:8787",
	)
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create TailscaleAdapter secret pipe: %w", err)
	}
	defer writePipe.Close()
	command.Stdin = readPipe
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		readPipe.Close()
		return fmt.Errorf("start TailscaleAdapter: %w", err)
	}
	_ = readPipe.Close()
	command.Stdin = nil
	adapterCommand = command
	done := make(chan struct{})
	adapterDone = done
	go func(running *exec.Cmd) {
		_ = running.Wait()
		adapterMu.Lock()
		if adapterCommand == running {
			adapterCommand = nil
			adapterDone = nil
		}
		adapterMu.Unlock()
		close(done)
		refreshAsync()
	}(command)
	payload := make([]byte, len(authKey)+1)
	copy(payload, authKey)
	payload[len(payload)-1] = '\n'
	_, writeErr := writePipe.Write(payload)
	zero(payload)
	closeErr := writePipe.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.WriteFile(stopFile, []byte("stop\n"), 0o600)
		return fmt.Errorf("send auth key to TailscaleAdapter: %v %v", writeErr, closeErr)
	}
	setText(authKeyEdit, "")
	return nil
}

func verifyAdjacentAdapter(directory string) error {
	catalogPath := filepath.Join(directory, "windowsagent-release.json")
	file, err := os.Open(catalogPath)
	if err != nil {
		return fmt.Errorf("open release catalog beside AssistGUI: %w", err)
	}
	catalog, loadErr := releasecatalog.Load(file)
	file.Close()
	if loadErr != nil {
		return loadErr
	}
	if err := releasecatalog.VerifySelected(directory, catalog, func(artifact releasecatalog.Artifact) bool {
		return artifact.Role == "tailscale-adapter"
	}); err != nil {
		return fmt.Errorf("verify TailscaleAdapter release artifact: %w", err)
	}
	return nil
}

func verifyAdapterProcess(status assistgui.TailscaleSnapshot) error {
	if status.ProcessID <= 0 {
		return fmt.Errorf("process ID is missing")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(status.ProcessID))
	if err != nil {
		return fmt.Errorf("open process %d: %w", status.ProcessID, err)
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return fmt.Errorf("read process image: %w", err)
	}
	dataDir, err := assistDataDir()
	if err != nil {
		return err
	}
	expected := filepath.Join(dataDir, "bin", "windows-tailscale-adapter.exe")
	actual := windows.UTF16ToString(buffer[:size])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("process image is %q, expected %q", actual, expected)
	}
	var adapterSession, guiSession uint32
	if err := windows.ProcessIdToSessionId(uint32(status.ProcessID), &adapterSession); err != nil {
		return fmt.Errorf("read adapter session: %w", err)
	}
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &guiSession); err != nil {
		return fmt.Errorf("read AssistGUI session: %w", err)
	}
	if adapterSession != guiSession {
		return fmt.Errorf("adapter session is %d, AssistGUI session is %d", adapterSession, guiSession)
	}
	return nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func stopTailscaleAdapter() error {
	dataDir, err := assistDataDir()
	if err != nil {
		return err
	}
	adapterMu.Lock()
	running := adapterCommand != nil && adapterCommand.Process != nil
	done := adapterDone
	adapterMu.Unlock()
	status := assistgui.LoadTailscaleStatus(dataDir)
	if status.State == assistgui.TailscaleDisabled {
		return nil
	}
	adapterActive := running
	if !adapterActive {
		adapterActive = verifyAdapterProcess(status) == nil
	}
	if status.State == assistgui.TailscaleFailed && !adapterActive {
		return fmt.Errorf("TailscaleAdapter is stopped in FAILED state: %s", status.Error)
	}
	stopFile := filepath.Join(dataDir, "tailscale", "stop.request")
	if err := os.MkdirAll(filepath.Dir(stopFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(stopFile, []byte("stop\n"), 0o600); err != nil {
		return fmt.Errorf("request TailscaleAdapter stop: %w", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status := assistgui.LoadTailscaleStatus(dataDir)
		if status.State == assistgui.TailscaleDisabled {
			if done != nil {
				select {
				case <-done:
					return nil
				default:
				}
			} else {
				return nil
			}
		}
		if status.State == assistgui.TailscaleFailed && verifyAdapterProcess(status) != nil {
			return fmt.Errorf("TailscaleAdapter stopped in FAILED state: %s", status.Error)
		}
		if done != nil {
			select {
			case <-done:
				status = assistgui.LoadTailscaleStatus(dataDir)
				return fmt.Errorf("TailscaleAdapter exited without confirming DISABLED: %s", status.Error)
			default:
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("TailscaleAdapter did not confirm logout and DISABLED state within 20 seconds")
}

func assistDataDir() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", fmt.Errorf("LOCALAPPDATA is required")
	}
	return filepath.Join(localAppData, "gameGuide", "windows-capture-agent"), nil
}

func showError(err error) {
	title, _ := windows.UTF16PtrFromString("WindowsAgent Assist")
	message, _ := windows.UTF16PtrFromString(err.Error())
	win.MessageBox(windowHandle, message, title, win.MB_OK|win.MB_ICONERROR)
}

func publish(text windowText) {
	textMu.Lock()
	pendingText = text
	textMu.Unlock()
	if windowHandle != 0 {
		win.PostMessage(windowHandle, refreshMessage, 0, 0)
	}
}
