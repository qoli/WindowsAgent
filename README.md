# WindowsAgent

**[Website](https://qoli.github.io/WindowsAgent/)** · [Design registry](docs/design/README.md) · [Security](SECURITY.md) · [Contributing](CONTRIBUTING.md)

WindowsAgent is a Go runtime for capabilities that must execute inside a
signed-in Windows user's interactive desktop session. It provides
foreground-aware screen capture, bounded observation and decision runtimes,
Rule-owned finite and streaming Actions, durable Action lifecycles, and
independent companion processes for events, evidence, inference, file transfer,
and delegated UI work.

WindowsAgent is not a general remote-memory API or an unrestricted desktop
control server. Generic Core code owns execution, isolation, validation, and
lifecycle. Executable-scoped Rules own game or application semantics,
coordinates, bindings, classifiers, and postconditions.

> [!WARNING]
> The Capture Agent listens on `0.0.0.0:8787` without authentication or TLS by
> default. Any client that can reach it can capture the desktop and invoke
> enabled Script, Action, process-execution, and input surfaces with the
> Agent's installed Windows token. The optional SFTP runtime similarly defaults
> to `0.0.0.0:2022` with SSH `none` authentication. Keep both listeners on a
> trusted LAN or private overlay network, and never expose them directly to the
> public Internet. See [Security boundaries](#security-boundaries).

## Contents

- [Current status](#current-status)
- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Build and validation](#build-and-validation)
- [Install and operate](#install-and-operate)
- [Deploy from macOS](#deploy-from-macos)
- [HTTP API](#http-api)
- [Rules and Actions](#rules-and-actions)
- [Security boundaries](#security-boundaries)
- [Project map](#project-map)

## Current status

The current public capability surface is summarized below. The
[design registry](docs/design/README.md) remains the canonical maturity view
for individual design documents; implementation and tests take precedence when
prose drifts.

| Capability | Current public state | Primary boundary |
| --- | --- | --- |
| WGC capture and foreground Rule resolution | Available | Capture Agent on `:8787` |
| Finite `windows-observation-v1` packages | Available | Isolated Job, Script Runner, and read-only Observer |
| `windows-pure-decision-v1` | Available | Permission-free in-process Starlark JSON mapping |
| Finite and streaming Rule Actions | Available | Rule v6 packages and durable invocation lifecycle |
| Ephemeral Action Sequences | Available | One preflighted sequence of 1–20 allowlisted Actions |
| Process and service inventory | Available | Parameter-free `GET /v1/processes` snapshot |
| Resident PP-OCR DirectML profiles | Available | Rule-declared residency while the Rule is active |
| Event Web and Action OSD | Available, optional | Independent read-only event projections |
| SFTP filesystem runtime | Available, optional | Separate SFTP-only process and Windows token |
| General Starlark automation | Partially available | Ephemeral `windows-starlark-action-v1` package |
| Direct process execution | Partially available | Structured `windows-exec-v1` operations |
| Direct and Rule-owned input | Partially available | Foreground-bound key and pointer runtimes |
| Event Stream | Partially available | Authenticated loopback append/replay journal |
| Evidence Recorder and Visual Log | Partially available | Independent finite recorder and passive inference producer |
| Delegated Pi agent | Partially available | Separate authenticated PI WEB control plane |
| ScreenParser Action | Partially available | One hash-pinned frame per DirectML invocation |
| Monitor and Reaction registration | Declaration and read-only catalog only | No scheduler or dispatcher is shipped |
| Mini reaction runtime | Draft only | No end-to-end runtime is shipped |

No Monitor or Reaction registration is active in the shipped Rules. A catalog
entry or `registrableAs` declaration is eligibility, not a running scheduler.
Retired module-registry, autonomous ScreenParser-loop, and scene-reducer designs
are historical references and are not current runtime paths.

### Capture guarantees

- Windows 10 1903+ amd64, primary-monitor WGC, and Direct3D 11.
- `native-jpeg` by default, with explicit `1080p-jpeg` and `native-png`
  profiles.
- HDR scRGB is tone-mapped to SDR before encoding.
- Cursor inclusion is selected per request.
- Each committed artifact includes a SHA-256 verified image, foreground process
  identity, capture time, and resolved Rule navigation.
- Capture failures remain explicit. There is no hidden GDI or alternate-provider
  fallback.
- The WGC worker is crash-isolated and keeps its capture resources resident
  across requests.

### Execution guarantees

- Observation packages are finite, schema-validated, permission-bounded, and
  isolated under one Windows Job Object.
- `windows-observer.exe` remains read-only and game-neutral.
- Pure decisions cannot access Observer, filesystem, native libraries, input,
  pointer, Action, or stream APIs.
- Finite Actions return one terminal result. Streaming Actions own their
  repeated observation, workflow state, cancellation, cleanup, compensation,
  durable events, and domain postconditions.
- Action Sequences compose existing Actions only. They have no variables,
  branches, loops, retries, nesting, persistence, or hidden provider changes.
- A completed input operation proves the bounded input event, not the
  application's later visual or domain state.

## Architecture

WindowsAgent separates capability ownership into six layers:

1. **Capture and foreground identity** — WGC captures the primary monitor,
   records the current foreground executable, resolves its Rule, and commits a
   verified artifact.
2. **Finite observation** — `windows-observation-v1` runs one declared Starlark
   package through the Script Runner and read-only Observer. Memory, file,
   screen-region, and native-library access are package-declared.
3. **Pure decisions** — `windows-pure-decision-v1` performs bounded internal
   JSON-to-JSON decisions without permissions or external access.
4. **Actions** — Rule v6 Action packages own executable application semantics.
   They declare finite return or durable stream completion.
5. **Ephemeral composition** — an allowlisted Action Sequence runs an immutable
   set of already-correct Actions after complete preflight.
6. **Independent processes** — Event Stream, Event Web, OSD, Evidence Recorder,
   Visual Log, SFTP, Watchdog, and delegated Pi retain their own process and
   lifecycle boundaries.

The Capture Agent may manage a resident inference worker while its owning Rule
is active, but worker residency is lifecycle configuration rather than an
Action, Monitor, or registration. The Watchdog is an external one-way lifecycle
owner; monitored modules do not depend on it and it does not recover itself.

## Quick start

Go 1.23 or newer is required. The .NET 8 SDK is required only for building the
self-contained ScreenParser and PP-OCR DirectML runtimes; it is not required on
the target Windows machine.

Build the repository-owned executables and copy the Rules beside them:

```bash
mkdir -p .build
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
./scripts/build-windows-capture-agent.sh
cp -R Rules .build/
```

Run the diagnostic console build from the signed-in Windows user's session:

```powershell
.\.build\windows-capture-agent-console.exe `
  --rules-dir (Resolve-Path .\.build\Rules)
```

Confirm health and create a capture:

```powershell
curl.exe http://127.0.0.1:8787/healthz

curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"include_cursor":true}' `
  http://127.0.0.1:8787/v1/captures
```

The Capture Agent must run in an interactive user session. Do not install it as
a traditional Session 0 Windows service; WGC cannot capture the signed-in
desktop from that context.

## Build and validation

Run checks proportional to the layer being changed. The full ordinary Go and
Rule validation is:

```bash
gofmt -w <touched-go-files>
git diff --check
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
./scripts/build-windows-capture-agent.sh
```

The build script emits and verifies the expected Windows PE subsystem for:

- the GUI Capture Agent and console diagnostic build;
- the persistent WGC worker;
- Action checker and local Starlark, exec, and key clients;
- observation Job, Script Runner, and Observer;
- Event Stream, Event Web, Action OSD, Watchdog, Evidence Recorder, Visual Log,
  and SFTP;
- the delegated Pi control and component executables.

`windows-capture-agent.exe` is the installable GUI artifact.
`windows-capture-agent-console.exe` is for interactive diagnostics and must not
replace the installed GUI build. Local clients such as `windows-exec.exe`,
`windows-key.exe`, `windows-starlark-check.exe`, and
`windows-starlark-invoke.exe` are not installed Agent payloads.

### Validate Action dependencies

`windows-action-check` is an offline development and release tool. It compiles
composite and streaming Starlark entrypoints, extracts static Action
dependencies, and rejects missing, cross-Rule, streaming-child, self, dynamic,
or cyclic calls.

```bash
go run ./cmd/windows-action-check --rules-dir Rules
go run ./cmd/windows-action-check --rules-dir Rules --json
```

Exit code `0` means valid, `1` means validation issues were found, and `2`
means the check could not run or write its report. The Capture Agent does not
run this checker at startup.

### Build DirectML runtime bundles

Build the ScreenParser runtime:

```bash
python3 tools/screenparser-runtime/publish.py \
  --dotnet "$(command -v dotnet)" \
  --output-dir "$PWD/.build/screenparser-directml"
```

Prepare the pinned PP-OCRv6 small artifacts and runtime:

```bash
python3 -m pip install -r tools/ppocr-model/requirements-build.in
python3 tools/ppocr-model/prepare.py \
  --output-dir "$PWD/.build/ppocrv6-small-w480" \
  --recognition-input-width 480
python3 tools/ppocr-runtime/publish.py \
  --dotnet "$(command -v dotnet)" \
  --output-dir "$PWD/.build/ppocr-directml"
```

Production PP-OCR and ScreenParser paths validate pinned model/runtime
artifacts and disable CPU execution-provider fallback. See the
[PP-OCR design](docs/design/ppocr-directml-runtime.md) and
[ScreenParser design](docs/design/screenparser-action.md).

## Install and operate

### Capture Agent

Install the persistent GUI build and external Rules tree:

```powershell
.\scripts\install-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe `
  -RulesPath .\.build\Rules `
  -OCRRuntimeBundlePath .\.build\ppocr-w480-bundle `
  -AgentRunLevel Limited
```

The installer creates interactive-user Scheduled Tasks, copies required
observation processes and Rule-declared runtime assets, verifies GUI PE
subsystems, and checks the Capture Agent and Event Stream health endpoints. It
does not create an SCM service or change Windows Firewall.

Use `-AgentRunLevel Highest` only when the installation intentionally requires
the installing user's elevated token. Packages cannot elevate themselves. For
an explicit development installation without Watchdog ownership, pass
`-StartupMode Standalone`.

Update only the installed Capture Agent binary set with the transactional
updater:

```powershell
.\scripts\update-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe
```

The updater verifies subsystem and hashes before replacement, retains the
previous binaries, probes the interactive listener, and restores the previous
set if the new runtime does not become healthy.

### Companion processes

Install the Action OSD after Event Stream is healthy:

```powershell
.\scripts\install-windows-action-osd.ps1 `
  -ExecutablePath .\.build\windows-action-osd.exe
```

The OSD is display-only, click-through, non-activating, and excluded from
capture by default. Its capture, recording, and Action indicators disclose
activity; they do not prove success.

Install the browser projection:

```powershell
.\scripts\install-windows-event-web.ps1 `
  -ExecutablePath .\.build\windows-event-web.exe
```

Event Web defaults to `http://127.0.0.1:8790/`, uses a browser token distinct
from the Event Stream token, and accepts only an explicit loopback or private
LAN IPv4 listener. It does not expose the loopback journal directly or alter
Windows Firewall.

Install the elevated SFTP-only filesystem process:

```powershell
.\scripts\install-windows-sftp.ps1 `
  -ExecutablePath .\.build\windows-sftp.exe
```

After verifying the generated Ed25519 host key out of band, connect with the
fixed protocol username:

```bash
sftp -P 2022 windowsagent@<windows-host>
```

The username does not select or impersonate a Windows account. Filesystem
permissions come from the dedicated Scheduled Task's Windows token. Shell,
exec, PTY, and forwarding requests are rejected.

Install the Evidence Recorder and Visual Log as independent resident control
processes:

```powershell
.\scripts\install-windows-observation-processes.ps1 `
  -EvidenceExecutablePath .\.build\windows-evidence-recorder.exe `
  -VisualLogExecutablePath .\.build\windows-visual-log.exe `
  -VisualLogModelBaseURL http://<model-host>:8001/v1
```

Evidence remains idle until an authenticated finite run is requested. A run
records 1080p H.264 at 1 FPS, defaults to 20 minutes, and accepts an explicit
duration from 1 through 3600 seconds. It has no extend, pause, manual stop, or
delete route. Visual Log passively consumes fresh PC-local Evidence frames and
appends untrusted descriptions; it cannot start, extend, or stop Evidence.

After installing all managed components, install an operator-authored Watchdog
configuration:

```powershell
.\scripts\install-windows-watchdog.ps1 `
  -ExecutablePath .\.build\windows-watchdog.exe `
  -ConfigPath .\watchdog-config.json
```

The Watchdog owns process availability and dependency order only. Its own
Scheduled Task has no automatic restart. A Watchdog crash leaves other modules
running and requires explicit diagnosis.

### Delegated Pi runtime

The delegated Pi runtime is a separate authenticated loopback control plane.
PI WEB owns its persistent SDK session and interactive questions; the Capture
Agent does not absorb that agent loop. Prepare its pinned Node environment and
install it using the commands in
[`runtimes/windows-agent-pi/README.md`](runtimes/windows-agent-pi/README.md).

### ScreenParser Action

Install the finite ScreenParser runtime and pinned model for the owning Rule:

```powershell
.\scripts\install-windows-screenparser.ps1 `
  -RulePath .\Rules\Palworld-Win64-Shipping.exe `
  -ModelPath C:\absolute\path\to\screenparser-v2.onnx `
  -RuntimeBundlePath C:\absolute\path\to\screenparser-directml
```

Each invocation processes one caller-supplied, hash-pinned RGB24 frame and
exits. It is not the retired autonomous ScreenParser loop.

## Deploy from macOS

### Deploy binaries

`scripts/deploy-windows-agent.sh` is the transactional macOS interface for the
twelve installed WindowsAgent binaries. It builds and hashes the payload,
reads the installed Scheduled Tasks and Watchdog configuration as the deployment
map, preserves their exact actions, replaces only mapped binaries, and restores
the previous verified set if replacement or health validation fails.

Always provide the intended SSH host explicitly in public or shared commands:

```bash
./scripts/deploy-windows-agent.sh --host <ssh-host>
./scripts/deploy-windows-agent.sh --host <ssh-host> --validate-only
```

`--validate-only` builds and transfers the candidate, verifies hashes, resolves
the installed map, and checks task, process, session, and HTTP health without
stopping or replacing the runtime. Both validation and publication write a JSON
receipt under `.build/binary-deployments/`. A dirty worktree is rejected unless
`--allow-dirty` is explicitly supplied.

The deployment script does not register Tasks, change triggers, rewrite the
Watchdog configuration, alter model endpoints, or start an Evidence or Visual
Log run.

### Deploy Rules

Publish the complete repository-owned Rules tree without rebuilding or
restarting the Agent:

```bash
python3 scripts/deploy-windows-rules.py --host <ssh-host>
python3 scripts/deploy-windows-rules.py --host <ssh-host> --validate-only
```

The deployer rejects symlinks, excludes known platform metadata, runs the
Action checker locally and on Windows, verifies a per-file hash manifest, and
publishes the complete tree transactionally. Unknown installed Rule directories
fail publication unless `--prune-unknown` is explicitly requested. Success
proves transport, hashes, installed catalogs, and Agent health; it does not
invoke an Action or prove a game-domain result.

## HTTP API

The Capture Agent surface on port `8787` is:

```text
GET  /healthz
GET  /v1/status
GET  /v1/processes
POST /v1/captures
GET  /v1/captures/latest
GET  /v1/captures/latest/content
GET  /v1/captures/{id}
GET  /v1/captures/{id}/content

GET  /v1/rules/{rule-id}/AGENTS.md
GET  /v1/rules/{rule-id}/scripts
GET  /v3/rules/{rule-id}/actions
GET  /v3/rules/{rule-id}/registrations
GET  /v3/rules/{rule-id}/action-sequence-tool
GET  /v4/rules/{rule-id}/runtimes

POST /v1/scripts/run
POST /v1/actions/invoke
POST /v1/action-sequences/invoke
POST /v1/starlark-actions/invoke
POST /v1/executions/invoke
POST /v1/key-inputs/invoke
GET  /v1/action-invocations/{invocation-id}
GET  /v1/action-invocations/{invocation-id}/events?after={cursor}
POST /v1/action-invocations/{invocation-id}/stop
```

### Process inventory

Read the current process and Service Control Manager snapshots without
parameters:

```powershell
curl.exe http://127.0.0.1:8787/v1/processes
```

Rows are generated on demand and are not cached or written to the event
journal. Services relate to processes only through matching nonzero PIDs. Zero
means that the service has no current hosting process; it is not joined to a
PID 0 process row. Unavailable enrichment fields are `null`, not guessed from
another provider.

### Capture and Rule discovery

Create a lossless capture and download its content:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"profile":"native-png","include_cursor":false}' `
  http://127.0.0.1:8787/v1/captures

curl.exe `
  -o capture.png `
  http://127.0.0.1:8787/v1/captures/latest/content
```

Every capture samples foreground identity after WGC produces the frame and
resolves the current executable against `Rules/`. A matched response links to
the Rule's guidance, Script catalog, Action catalog, registrations, sequence
schema, and runtime profiles. An unmatched executable remains explicitly
unmatched. Missing foreground process identity fails the request; the Agent
does not guess it or commit a partial artifact.

Only one capture may run at a time. A concurrent request receives
`409 capture_busy`.

### Invoke Actions

Discover the current Action catalog from the matched Rule, then invoke one
schema-valid Action:

```powershell
curl.exe http://127.0.0.1:8787/v3/rules/CrimsonDesert.exe/actions

curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"actionId":"crimson-desert/inventory","inputs":{}}' `
  http://127.0.0.1:8787/v1/actions/invoke
```

A finite Action returns HTTP `200` with terminal output. A streaming Action
returns HTTP `202`, a durable invocation ID, and watch information. Follow the
returned NDJSON event URL until completion, failure, or cancellation. A stop
target is present only when the Action declares itself interruptible.

For an ephemeral plan, fetch
`/v3/rules/{rule-id}/action-sequence-tool` and send its schema-valid arguments
to `/v1/action-sequences/invoke`. All steps are preflighted before the first
Action starts.

### Invoke general Windows operations

Upload and invoke one ephemeral Starlark package:

```bash
go run ./cmd/windows-starlark-invoke \
  --url http://<windows-host>:8787 \
  --package docs/examples/windows-starlark-hello \
  --inputs /absolute/path/to/inputs.json
```

The local client validates and packages the request, then prints the remote
JSON unchanged. An accepted HTTP `202` proves only that Windows accepted the
invocation; inspect the durable terminal result and declared postconditions.

Run one structured process operation without a Starlark package:

```bash
go run ./cmd/windows-exec run \
  --url http://<windows-host>:8787 \
  --executable whoami.exe
```

Arguments remain individual CLI and JSON array elements. The client does not
insert a command string, switch to SSH or another runtime, or interpret a
nonzero child exit as transport failure. Captured stdout and stderr are each
limited to 65,536 bytes by default and may only be configured lower; exceeding
the limit terminates the owned process tree with an explicit
`EXEC_OUTPUT_LIMIT_EXCEEDED` failure. Large logs belong in an explicitly named
Windows file retrieved through the separate SFTP data plane, while the process
result returns only bounded metadata.

Send one foreground-pinned scan-code press using identity from a fresh capture:

```bash
go run ./cmd/windows-key press \
  --url http://<windows-host>:8787 \
  --key Key_Home \
  --hold 180ms \
  --expected-process-id 1234 \
  --expected-executable-name Game.exe \
  --expected-executable-path 'C:\Games\Game.exe'
```

Foreground mismatch or drift, key conflicts, injection failure, and release
failure remain explicit `INPUT_*` errors. Completion is evidence of the key
operation only.

### Companion APIs

These independent processes do not extend the unauthenticated Capture Agent
listener:

| Process | Default listener | Authentication | Surface |
| --- | --- | --- | --- |
| Event Stream | `127.0.0.1:8788` | Bearer token except health | append, replay, time range, NDJSON stream |
| Event Web | `127.0.0.1:8790` | distinct browser bearer token | browser timeline and exact OSD projection |
| Visual Log | `127.0.0.1:8789` | bearer token except health | read-only status |
| Evidence Recorder | `127.0.0.1:8792` | bearer token except health | finite runs, status, UTC range ZIP, contact sheet |
| SFTP health | `127.0.0.1:8793` | none | health only |
| SFTP | `0.0.0.0:2022` | SSH `none`; pinned host identity | filesystem operations only |

Event and Evidence token files, journals, captures, video, OCR results, model
keys, delegated-task data, and logs are private operator state. Do not commit
or publish them.

## Rules and Actions

`Rules/<Executable.exe>/rule.json` is the source of truth for that executable's
current Actions, runtime profiles, sequence allowlist, and registration
declarations. Each Action package owns its `TASK.md`, schemas, manifest,
implementation, coordinates, binding source, classifier, and postcondition.

The repository currently ships Rules for:

| Executable | Responsibility |
| --- | --- |
| `CrimsonDesert.exe` | finite inventory observation |
| `Cyberpunk2077.exe` | foreground-bound pointer operations |
| `EliteDangerous64.exe` | finite observations, resident OCR, binding-resolved input, and supervised streaming workflows |
| `Palworld-Win64-Shipping.exe` | finite ScreenParser UI-element inference |

Do not treat this table as an Action catalog. Read the files on disk during
development or the live Rule endpoints while operating an installed Agent.
Request-time Rule loading means a valid Rule-tree publication does not require
an Agent restart.

New or changed observation packages must follow the
[Script Package development contract](docs/script-development-contract.md).
New or changed Actions require loader, behavior, negative-path, schema,
manifest, and dependency validation. Agent-facing Rule guidance must also pass
the [OpenCode black-box acceptance contract](docs/testing/opencode-black-box-acceptance-contract.md).

## Security boundaries

- Network reachability is the trust boundary for port `8787`. Structured
  requests and foreground validation improve correctness; they do not provide
  authentication or authorization.
- SFTP accepts no client credential. Verify its persistent host key out of band
  and restrict network reachability independently.
- Event, Web, Visual Log, Evidence, and delegated Pi processes use separate
  authenticated control planes. Do not reuse their tokens or expose loopback
  listeners through the Capture Agent.
- The installers do not alter Windows Firewall.
- Do not run the Capture Agent as a traditional Windows service.
- Captures and logs may disclose process paths, usernames, window titles,
  visible documents, game state, or account information.
- Crash dumps can contain process memory. Keep them local and bounded.
- Rules are trusted local packages. The public repository must not contain
  credentials, private hosts, screenshots, save files, memory dumps, OCR
  results, event journals, or operator configuration.
- There is no silent fallback to another capture backend, execution provider,
  model, precision, Rule, Action, decoder, binding, or algorithm.

Read [SECURITY.md](SECURITY.md) before deployment or vulnerability reporting.

## Project map

This is an ownership map rather than an exhaustive directory inventory:

```text
cmd/                          thin executable composition and client entrypoints
internal/capture*/            capture contracts, artifacts, and activity signal
internal/wgc*/                persistent WGC worker and versioned worker protocol
internal/foreground/          foreground process observation
internal/rules/               Rule loading and live navigation
internal/observation*/        finite observation Job, launcher, and protocols
internal/scriptrunner/        isolated Starlark and generic native-library FFI
internal/puredecision/        permission-free JSON decision runtime
internal/action*/             Action loading, lifecycle, checks, and sequences
internal/*input*/             foreground-bound keyboard input ownership
internal/*pointer*/           foreground-bound pointer operations
internal/ocr*/                raw OCR Action and resident-worker contracts
internal/event*/              event journal clients, HTTP API, and Web projection
internal/evidence*/           finite recording lifecycle and authenticated API
internal/visuallog*/          passive visual-log producer and status API
internal/delegated*/          delegated-task lifecycle and host-facing API
internal/piweb/               PI WEB HTTP and WebSocket adapter
internal/sftpruntime/         SFTP-only server, host identity, and health
internal/watchdog/            external process probes and bounded recovery
internal/windowsautomation/   ephemeral general Windows Starlark runtime
internal/windowsexec/         structured process and PowerShell-file runtime
Rules/<Executable.exe>/       distributable Rule v6 packages and guidance
runtimes/                     self-contained external inference and Pi runtimes
tools/                        model preparation, publishing, and diagnostics
scripts/                      install, update, deploy, and operator helpers
docs/design/                  maintained maturity registry and design documents
docs/protocol/                runtime protocol usage
docs/testing/                 black-box and external acceptance contracts
```

Core capabilities belong in distinct `internal` packages with explicit API and
runtime contracts. Game or application semantics remain in the owning Rule.

## Contributing and license

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and validation rules and
[SECURITY.md](SECURITY.md) for private vulnerability reporting. WindowsAgent is
available under the [MIT License](LICENSE).
