package assistbackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/qoli/WindowsAgent/internal/assistgui"
	"github.com/qoli/WindowsAgent/internal/assistlifecycle"
	"github.com/qoli/WindowsAgent/internal/assisttailscale"
	"github.com/qoli/WindowsAgent/internal/releasecatalog"
	"github.com/qoli/WindowsAgent/internal/releasedownload"
)

const latestCatalogURL = "https://github.com/qoli/WindowsAgent/releases/latest/download/windowsagent-release.json"

type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func Execute(ctx context.Context, request Request, emitter *Emitter) error {
	dataDir, err := dataDirectory()
	if err != nil {
		return fail("ENVIRONMENT_INVALID", err)
	}
	progress := func(phase, message string) error {
		return emitter.Emit(Event{Type: "progress", Phase: phase, Message: message})
	}
	switch request.Command {
	case CommandInspect:
		if err := progress("inspect", "Reading installed WindowsAgent state"); err != nil {
			return err
		}
		snapshot, err := inspect(ctx, dataDir)
		if err != nil {
			return fail("INSPECTION_FAILED", err)
		}
		if err := emitter.Emit(Event{Type: "snapshot", Snapshot: &snapshot}); err != nil {
			return err
		}
		return emitter.Emit(successEvent("Inspection completed", false))
	case CommandInstall, CommandUpdate, CommandRepair:
		if err := progress("download", "Downloading and verifying the WindowsAgent release"); err != nil {
			return err
		}
		stage, catalogPath, err := stageRelease(ctx, dataDir)
		if err != nil {
			return fail("RELEASE_STAGE_FAILED", err)
		}
		startAtSignIn, err := requestedWatchdogSetting(ctx, request, dataDir)
		if err != nil {
			return fail("INSPECTION_FAILED", err)
		}
		if err := progress("handoff", "Starting the elevated setup transaction"); err != nil {
			return err
		}
		if err := startApplyHandoff(string(request.Command), stage, catalogPath, dataDir, startAtSignIn, request.ClientProcessID, os.Getpid()); err != nil {
			return fail("ELEVATED_HANDOFF_FAILED", err)
		}
		return emitter.Emit(successEvent("Elevated setup handoff started", true))
	case CommandUninstall:
		if err := progress("handoff", "Starting the elevated uninstall transaction"); err != nil {
			return err
		}
		if err := startUninstallHandoff(dataDir, request.ClientProcessID, os.Getpid()); err != nil {
			return fail("ELEVATED_HANDOFF_FAILED", err)
		}
		return emitter.Emit(successEvent("Elevated uninstall handoff started", true))
	case CommandConfigureWatchdog:
		if err := progress("configure-watchdog", "Starting the elevated Watchdog configuration update"); err != nil {
			return err
		}
		if err := startConfigureWatchdogHandoff(dataDir, *request.Settings.WatchdogStartAtSignIn); err != nil {
			return fail("ELEVATED_HANDOFF_FAILED", err)
		}
		return emitter.Emit(successEvent("Elevated Watchdog configuration handoff started", false))
	case CommandStart:
		if err := progress("start", "Starting WindowsAgent through Watchdog"); err != nil {
			return err
		}
		if _, err := assistlifecycle.Start(ctx, dataDir); err != nil {
			return fail("WINDOWS_AGENT_START_FAILED", err)
		}
		if request.Settings != nil && request.Settings.TailscaleAuthKey != "" {
			secret := []byte(request.Settings.TailscaleAuthKey)
			request.Settings.TailscaleAuthKey = ""
			if err := progress("start-tailscale", "Starting TailscaleAdapter"); err != nil {
				zero(secret)
				return err
			}
			if _, err := assisttailscale.Start(ctx, dataDir, secret); err != nil {
				return fail("TAILSCALE_START_FAILED", err)
			}
		}
	case CommandStop:
		if err := progress("stop", "Stopping Watchdog and verified WindowsAgent processes"); err != nil {
			return err
		}
		if _, err := assistlifecycle.Stop(ctx, dataDir); err != nil {
			return fail("WINDOWS_AGENT_STOP_FAILED", err)
		}
	case CommandStartTailscale:
		secret := []byte(request.Settings.TailscaleAuthKey)
		request.Settings.TailscaleAuthKey = ""
		if err := progress("start-tailscale", "Starting TailscaleAdapter"); err != nil {
			zero(secret)
			return err
		}
		if _, err := assisttailscale.Start(ctx, dataDir, secret); err != nil {
			return fail("TAILSCALE_START_FAILED", err)
		}
	case CommandStopTailscale:
		if err := progress("stop-tailscale", "Logging out and stopping TailscaleAdapter"); err != nil {
			return err
		}
		if _, err := assisttailscale.Stop(ctx, dataDir); err != nil {
			return fail("TAILSCALE_STOP_FAILED", err)
		}
	default:
		return fail("REQUEST_INVALID", fmt.Errorf("unsupported command %q", request.Command))
	}
	snapshot, err := inspect(ctx, dataDir)
	if err != nil {
		return fail("POSTCONDITION_FAILED", err)
	}
	if err := emitter.Emit(Event{Type: "snapshot", Snapshot: &snapshot}); err != nil {
		return err
	}
	return emitter.Emit(successEvent("Command completed", false))
}

func EmitError(emitter *Emitter, err error) error {
	code := "INTERNAL_ERROR"
	var coded *codedError
	if errors.As(err, &coded) {
		code = coded.code
	}
	return emitter.Emit(Event{Type: "error", Code: code, Message: err.Error()})
}

func EmitRequestError(emitter *Emitter, err error) error {
	return emitter.Emit(Event{Type: "error", Code: "REQUEST_INVALID", Message: err.Error()})
}

func fail(code string, err error) error { return &codedError{code: code, err: err} }

func dataDirectory() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", errors.New("LOCALAPPDATA is required")
	}
	return filepath.Clean(filepath.Join(localAppData, "gameGuide", "windows-capture-agent")), nil
}

func inspect(ctx context.Context, dataDir string) (Snapshot, error) {
	agent, err := (assistgui.Inspector{DataDir: dataDir, AgentURL: "http://127.0.0.1:8787/healthz", Port: "8787"}).Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	agent.Tailscale = assisttailscale.Inspect(ctx, dataDir)
	lifecycle, err := assistlifecycle.Inspect(ctx, dataDir)
	if err != nil {
		lifecycle = assistlifecycle.Facts{Installed: agent.Installed}
	}
	version := agent.InstalledVersion
	if version == "" {
		version = lifecycle.Version
	}
	var versionPointer *string
	if version != "" {
		versionPointer = &version
	}
	result := Snapshot{
		Installed:    agent.Installed,
		Version:      versionPointer,
		Capture:      CaptureSnapshot{Running: agent.AgentHealthy},
		Watchdog:     WatchdogSnapshot{Installed: lifecycle.WatchdogInstalled, Running: lifecycle.WatchdogRunning, StartAtSignIn: lifecycle.WatchdogStartAtLogon},
		LANEndpoints: []string{},
		Tailscale:    tailscaleSnapshot(agent.Tailscale),
	}
	for _, endpoint := range agent.LANEndpoints {
		result.LANEndpoints = append(result.LANEndpoints, endpoint.URL)
	}
	return result, nil
}

func requestedWatchdogSetting(ctx context.Context, request Request, dataDir string) (bool, error) {
	if request.Settings != nil && request.Settings.WatchdogStartAtSignIn != nil {
		return *request.Settings.WatchdogStartAtSignIn, nil
	}
	if request.Command == CommandInstall {
		return true, nil
	}
	facts, err := assistlifecycle.Inspect(ctx, dataDir)
	if err != nil {
		return false, err
	}
	return facts.WatchdogStartAtLogon, nil
}

func stageRelease(ctx context.Context, dataDir string) (string, string, error) {
	downloader := releasedownload.Client{HTTP: releasedownload.NewHTTP1Client()}
	catalog, base, err := downloader.FetchCatalog(ctx, latestCatalogURL)
	if err != nil {
		return "", "", err
	}
	stage := filepath.Join(dataDir, "release-staging", catalog.Version+"-"+fmt.Sprint(time.Now().UTC().UnixNano()))
	if err := downloader.Stage(ctx, base, catalog, stage, releasecatalog.InstallArtifact); err != nil {
		return "", "", err
	}
	catalogPath := filepath.Join(stage, "windowsagent-release.json")
	if err := writeMetadata(catalogPath, filepath.Join(stage, "SHA256SUMS"), catalog); err != nil {
		return "", "", err
	}
	return stage, catalogPath, nil
}

func writeMetadata(catalogPath, sumsPath string, catalog releasecatalog.Catalog) error {
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

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func successEvent(message string, closeGUI bool) Event {
	success := true
	return Event{Type: "result", Success: &success, Message: message, CloseGUI: closeGUI}
}

func tailscaleSnapshot(status assistgui.TailscaleSnapshot) TailscaleSnapshot {
	state := "error"
	switch status.State {
	case assistgui.TailscaleDisabled:
		state = "disabled"
	case assistgui.TailscaleStarting:
		state = "starting"
	case assistgui.TailscaleOnline:
		state = "online"
	case assistgui.TailscaleStopping:
		state = "stopping"
	case assistgui.TailscaleFailed:
		state = "error"
	}
	return TailscaleSnapshot{Status: state, IPv4: optionalString(status.IPv4), IPv6: optionalString(status.IPv6)}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
