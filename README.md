# WindowsAgent

WindowsAgent is an extensible Go agent for capabilities that must run inside a
signed-in Windows user's interactive session.

Its first capability is primary-monitor still capture through Windows Graphics
Capture (WGC). The project is intentionally broader than screenshots: future
process inspection, memory observation, value scanning, and other
Cheat Engine-inspired capabilities can be added behind explicit package, API,
permission, and safety boundaries.

> [!WARNING]
> The current HTTP server listens on `0.0.0.0:8787` without authentication,
> TLS, or CORS by default. Anyone who can reach that port can trigger and
> download screenshots and read foreground process metadata, including window
> titles and executable paths. Use it only on a trusted LAN or private overlay
> network.

## Status

The screenshot capability is available today:

- Windows 10 1903+ amd64
- primary-monitor capture using WGC and Direct3D 11
- SDR PNG output
- HDR scRGB capture tone-mapped to an SDR PNG preview
- cursor inclusion selected per request
- foreground process ID, executable name/path, window title, and observation time
  recorded with each capture
- capture-time foreground rule resolution with a navigable Codex `AGENTS.md`
- SHA-256 verified artifacts and bounded retention
- strict JSON errors with no GDI or hidden capture fallback
- optional hidden startup through an interactive-user Scheduled Task

General process enumeration and memory capabilities are not implemented yet.

## Build

Go 1.23 or newer is required.

```bash
mkdir -p .build
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath \
  -o .build/windows-capture-agent.exe \
  ./cmd/windows-capture-agent
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-H=windowsgui" \
  -o .build/windows-capture-agent-background.exe \
  ./cmd/windows-capture-agent
```

The GUI-subsystem build runs without a console window.

## Run

Run the console build inside the signed-in Windows user's session:

```powershell
.\.build\windows-capture-agent.exe
```

Available options:

```text
--listen              HTTP listen address (default 0.0.0.0:8787)
--data-dir            artifact and log root
--capture-timeout     per-request timeout (default 5s)
--retention           number of artifacts to retain (default 100)
--log-level           debug, info, warn, or error
--log-file            optional JSON log file
```

The process must not run as a traditional Session 0 Windows service because WGC
requires access to the interactive desktop.

## Optional persistent startup

From the repository root in PowerShell:

```powershell
.\scripts\install-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent-background.exe
```

The installer copies the executable under the current user's
`%LOCALAPPDATA%`, registers an interactive-token at-logon Scheduled Task, starts
it, and verifies `/healthz`. It does not create an SCM service or modify Windows
Firewall.

For deployment compatibility, the current executable, task, and data directory
retain their established `windows-capture-agent` names. They identify the first
capability, not the broader project.

## HTTP API

```text
GET  /healthz
GET  /v1/status
POST /v1/captures
GET  /v1/captures/latest
GET  /v1/captures/latest/content
GET  /v1/captures/{id}
GET  /v1/captures/{id}/content
GET  /v1/rules/{rule-id}/AGENTS.md
```

Create a capture:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"include_cursor":true}' `
  http://127.0.0.1:8787/v1/captures
```

Download the latest PNG:

```powershell
curl.exe `
  -o capture.png `
  http://127.0.0.1:8787/v1/captures/latest/content
```

Only one capture can run at a time. A concurrent request receives
`409 capture_busy`. Each completed artifact contains `capture.png` and
`metadata.json`. The response and metadata include a required `foreground`
object:

```json
{
  "foreground": {
    "observed_at": "2026-07-27T01:02:03.000000004Z",
    "process_id": 4242,
    "executable_name": "Game.exe",
    "executable_path": "C:\\Games\\Game.exe",
    "window_title": "Game"
  },
  "rule": {
    "status": "matched",
    "description": "The executing agent must read rule.agents.url before taking any rule-specific action.",
    "id": "Game.exe",
    "agents": {
      "url": "/v1/rules/Game.exe/AGENTS.md",
      "content_type": "text/markdown; charset=utf-8",
      "sha256": "..."
    }
  }
}
```

The foreground window is sampled immediately after WGC produces the captured
frame. The same response resolves its executable name against the trusted,
embedded folders under `Rules/`; this keeps the capture JSON as Codex's single
Windows perception entry point. Codex can follow `rule.agents.url` to read the
matched process guidance. An executable without a rule reports
`rule.status=unmatched` with a description that no rule guidance is available,
without inventing a substitute.

If Windows does not expose the foreground process or its executable
path, the request fails explicitly with `503 foreground_process_unavailable`;
the agent does not guess process identity or commit a partial artifact.

Foreground and rule metadata are required for every artifact under this
contract. Artifact directories created by older builds do not contain the full
contract and fail the strict startup scan rather than being presented as
complete captures. Preserve or archive those directories before installing
this build with a new, empty data directory; there is no automatic migration.

## Project layout

```text
cmd/windows-capture-agent/       screenshot capability executable
docs/design/                     maintained design registry
internal/artifact/               artifact transactions and retention
internal/capture/                screenshot capability contracts
internal/config/                 process configuration
internal/foreground/             foreground process observation
internal/httpapi/                current HTTP surface
internal/pixels/                 SDR and HDR pixel conversion
internal/rules/                  executable rule registry and navigation
internal/wgc/                    WGC and Direct3D 11 implementation
Rules/                           trusted per-process Codex guidance
scripts/                         Windows installation helpers
```

New capabilities should receive their own internal package and API contract
instead of being folded into the screenshot packages.

## Security and contributions

See [SECURITY.md](SECURITY.md) before exposing the listener or reporting a
vulnerability. Contributions are described in
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

WindowsAgent is available under the [MIT License](LICENSE).
