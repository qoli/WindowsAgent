# windows-agent-pi runtime bundle

This directory pins the external runtime used by the independent
`windows-agent-pi.exe` control plane. It does not embed Pi, PI WEB, or
computer-use into the WindowsAgent capture process.

The three processes run in the signed-in Windows user's interactive session:

1. `pi-web-sessiond` owns Pi SDK sessions and loads the configured
   `@injaneity/pi-computer-use` Pi package.
2. `pi-web-server` exposes the loopback Web UI and session API on port 8504.
3. `windows-agent-pi.exe` exposes the authenticated, durable delegated-task API
   on port 8791 and adapts each task to one PI WEB session.

Use a dedicated profile and data root. The exact local paths are operator
configuration and must not be committed:

```powershell
$env:PI_CODING_AGENT_DIR = "C:\absolute\windows-agent-pi\pi-agent"
$env:PI_CODING_AGENT_SESSION_DIR = "C:\absolute\windows-agent-pi\sessions"
$env:PI_WEB_DATA_DIR = "C:\absolute\windows-agent-pi\pi-web"
$env:PI_WEB_CONFIG = "C:\absolute\windows-agent-pi\pi-web\config.json"
```

After `npm ci`, install the pinned computer-use package into that active Pi
profile once with `npm run pi:install-computer-use`. Missing package setup or a
missing helper is an explicit runtime failure; no alternative computer-use
backend is selected.

From a Windows checkout, copy the source Pi `auth.json` and `settings.json` to a
private staging directory. The preparation script migrates only model-facing
settings, deliberately drops macOS package paths, installs the one pinned
computer-use package, and prepares the helper. Then install the three
current-user processes:

```powershell
$runtimeDir = (Resolve-Path .\runtimes\windows-agent-pi).Path
.\scripts\prepare-windows-agent-pi-runtime.ps1 `
  -RuntimeBundlePath $runtimeDir `
  -SourceAuthPath "C:\private-staging\auth.json" `
  -SourceSettingsPath "C:\private-staging\settings.json"

.\scripts\install-windows-agent-pi.ps1 `
  -ExecutablePath (Resolve-Path .\.build\windows-agent-pi.exe) `
  -ComponentExecutablePath (Resolve-Path .\.build\windows-agent-pi-component.exe) `
  -RuntimeBundlePath $runtimeDir `
  -PiWebConfigPath (Join-Path $env:LOCALAPPDATA "gameGuide\windows-agent-pi\pi-web\config.json")
```

The installer performs no package download and no helper build. It verifies the
lock-derived immutable runtime, exact package versions, prebuilt helper,
dedicated Pi profile, loopback listeners, PI WEB component/runtime versions,
and signed-in interactive process sessions before returning its JSON receipt.
It creates three independent limited current-user Scheduled Tasks and never
changes Windows Firewall. The gateway is a GUI-subsystem background executable,
and the two PowerShell component launchers explicitly use a hidden window, so
the persistent runtime does not occupy the interactive desktop it controls.

On macOS, use `scripts/windows-agent-pi-client.sh --ssh-host <user@host>` with
`submit`, `status`, `watch`, `message`, `cancel`, or `web`. The client tunnels
both loopback ports over SSH and reads the installer-owned bearer token without
printing it. `web` is the intentional answer path for PI questions and
extension dialogs; the delegated API does not proxy their schemas.

Signed-in Windows acceptance has verified the Kimi model authorization,
computer-use discovery/observation/button input against an existing Calculator
window, a visible `1591` postcondition, durable task completion, cancellation,
and clean component process-tree stop/restart. The external
`pi-computer-use@0.5.1` Windows backend could not open an absent desktop app in
this run: ref-less `keypress` was rejected despite its optional-ref schema, and
Explorer-backed refs became stale. Application launch was therefore an
explicit test-fixture setup through `windows-exec`, not silently presented as
computer-use success.
