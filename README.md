# WindowsAgent

**[Website](https://qoli.github.io/WindowsAgent/)** · [Design docs](docs/design/README.md) · [Contributing](CONTRIBUTING.md)

WindowsAgent is an extensible Go agent for capabilities that must run inside a
signed-in Windows user's interactive session.

Its first capability is primary-monitor still capture through Windows Graphics
Capture (WGC). It also includes a finite, read-only observation-job runtime
that brokers locally distributed Starlark Script Packages to a
unified memory/file/screen-region observer process. Script Packages may carry
manifest-declared native DLLs and call them through the Script Runner's generic
Windows amd64 FFI. Every capability remains behind an explicit package, API,
permission, and validation boundary.

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
- native-resolution JPEG Q90 4:4:4 output by default
- explicit `1080p-jpeg` and lossless `native-png` request profiles
- HDR scRGB capture tone-mapped to an SDR image before encoding
- cursor inclusion selected per request
- foreground process ID, executable name/path, window title, and observation time
  recorded with each capture
- capture-time foreground rule resolution with a navigable Codex `AGENTS.md`
- SHA-256 verified artifacts and bounded retention
- strict JSON errors with no GDI or hidden provider fallback; one
  crash-isolated worker generation keeps its WGC session, D3D11
  device/context, frame pool, and region shader resident across requests
- a failed worker call is never replayed; that generation is retired and only
  a later independent request may start a fresh generation
- optional hidden startup through an interactive-user Scheduled Task

The generic Starlark launcher and finite Script capabilities are available
today:

- a Script `screen.readRegion` whose Observer transport exits with the exact
  broker EOF signature may restart its isolated launch up to five times, with
  a bounded delay between attempts; unrelated and exhausted failures remain
  explicit and terminal

- `crimson-desert/inventory` performs a finite memory attempt and, only when
  that attempt cannot produce a valid inventory, discovers and decodes the
  newest unambiguous save file inside its package-declared LocalAppData root
- `elite-dangerous/compass` reads one fixed 96x96 reference-density region in
  the centered 1920x1080 coordinate space and returns the cyan target marker's
  reference-coordinate offset, clockwise screen angle, Euclidean center
  distance, and circular center-zone membership
- `elite-dangerous/ship-status` composes a reference-density PP-OCR boxes
  Action with a pure game classifier; it confirms only `MASS`, `LANDING`, and
  `CARGO`, then independently reports the three same-frame indicators as
  `ON`, `OFF`, or evidence-preserving `UNKNOWN`
- `elite-dangerous/ship-speed` reads the fixed visual HUD speed-number region
  and classifies qualified evidence as `STOPPED` (`0`), `LOW_SPEED` (`1-9`),
  or `MOVING` (`>=10`). Only `MOVING` exposes its non-zero `displayValue`;
  covered or ambiguous values remain `UNKNOWN` without consulting journal,
  status-file, or throttle-command state
- `elite-dangerous/flight-status` accepts the complete raw output of
  `elite-dangerous/flight-prompt-text`, combines OCR confidence with finite
  phrase similarity, and returns one reviewed flight state or `UNKNOWN`,
  including the explicit `SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` Gate for
  `MOVE TO OBTAIN LINE OF SIGHT TO TARGET`
- the Go launcher resolves any registered `windows-observation-v1` capability
  from its owning Rule, validates its input schema and package resource
  declarations,
  and never contains a capability allowlist
- the job returns one schema-validated JSON result with per-call provenance
- the Go host launches only the runner and observer directly under one bounded
  Windows Job Object; it does not use PowerShell or a polling loop
- `windows-observer.exe` remains game-neutral and never loads DLLs
- the generic screen observer captures once without a cursor, maps a 1920x1080
  reference rectangle through the centered 16:9 viewport, performs bounded
  `reference` or `native` GPU region sampling, and leaves UI interpretation to
  the owning package
- screen-region sampling requires D3D11 compute shader model 5.0 and the
  Windows `d3dcompiler_47.dll`; missing shader support fails the Action and
  never falls back to a full-frame CPU path
- the save file becomes a job-scoped opaque blob; the Script Runner resolves
  that blob and loads only the package-declared DLL alias

This is not a general remote memory API. HTTP exposes a live read-only Script
catalog; the unauthenticated run endpoint delegates one strictly validated
request to the local launcher inside the signed-in Windows session.

The Action runtime and registration refactor is partially landed:

- Rule schema version 6 declares executable Actions, an explicit ephemeral
  sequence allowlist, explicit return or stream
  completion, optional resident runtime
  profiles, and separately registers selected Actions as timer-driven Monitors
  or event-driven Reactions;
- `windows-event-stream.exe` owns a strict append-only JSONL journal and an
  authenticated loopback append/replay/time-range API;
- `windows-visual-log.exe` is an optional independent producer that warms one
  exactly configured oMLX model, reads the newest frame from the Evidence
  recorder's PC-local shared-memory tap on its own loop tick, and appends an
  untrusted timestamped scene description. Invalid model output
  drops only that sample; it never controls evidence recording or substitutes
  another model, capture profile, or prior description;
- `POST /v1/actions/invoke` gives every call an invocation ID. Finite Actions
  return terminal output directly; streaming Actions first commit a durable
  start event and immediately return a callback URL, optional stop URL, and
  their declared linear or loop lifecycle;
- `run_action_sequence` is generated per Rule as a strict JSON function schema.
  It preflights and immediately runs one immutable sequence of 1–20 allowlisted
  Actions in order, with no variables, branches, loops, nesting, or persisted
  executable definition;
- streaming Starlark exposes strict `action.call`, explicit `action.try_call`,
  and bounded failure compensation registration. `action.try_call` returns
  `{ok, output, error, errorCode}` so a workflow may
  emit and bound a failed observation sample without changing providers or
  silently converting an execution failure into domain `UNKNOWN`.
  `action.on_failure` registers child Actions that run only when the streaming
  Action fails. Optional `critical=True` compensations run before ordinary
  compensations, and every registration has its own bounded
  `timeout_milliseconds` budget; reverse registration order is preserved within
  each class. `action.clear_on_failure` removes them after the protected state
  has been restored. On Agent startup, durable invocations missing a terminal
  event are failed explicitly with `ABORTED_BY_AGENT_RESTART` and remain
  queryable/watchable rather than being resumed against unknown game state;
- Crimson Desert inventory remains a finite Action using the landed v1
  observation runtime;
- `screenparser/ui-elements` is a Palworld-configured on-demand Action
  that transforms one caller-supplied, hash-pinned RGB24 frame through the
  verified FP16 ScreenParser v2 ONNX model and then exits;
- Elite Dangerous declares a Rule-resident `ocr/w480` DirectML worker and the
  finite `elite-dangerous/flight-prompt-text` Action. The Action captures one
  reviewed 400x40 reference-density region and returns raw OCR text, confidence,
  provenance, model identity, and timing;
- its separate pure `elite-dangerous/flight-status` Action classifies that raw
  output into a finite status only when both combined-confidence and
  best-candidate-margin thresholds pass; unresolved content remains `UNKNOWN`;
- Elite Dangerous also declares `ocr/text-regions`, a resident PP-OCRv6 small
  detection-plus-recognition profile. The generic raw Action returns text
  quadrilaterals, recognition evidence, and bounded same-frame left context;
  the composite `elite-dangerous/ship-status` Action alone owns its three
  lower-right indicator semantics;
- the composite `elite-dangerous/ship-speed` Action uses that same resident
  text-regions profile over a separate reference ROI. It is eligible for
  opt-in Monitor or Reaction registration, but no speed loop is active by
  default;
- `windows-key-action-v1` is a game-neutral finite runtime for a serialized,
  foreground-bound scan-code press or one leased non-blocking hold. Hold
  packages expose explicit `START`, `RENEW`, and `STOP`; expiry, failure
  compensation, and Agent shutdown release the exact resolved key. A Rule
  package may declare literal canonical keys directly or select a game-specific
  binding source; callers still choose only schema-valid logical selections;
- `elite-dangerous/ui-control` performs exactly one model-selected logical UI
  movement or selection. It is intentionally a slow screenshot/one-key
  interaction surface for tasks such as arranging `AUTO LAUNCH`;
- `elite-dangerous/set-throttle` resolves `SetSpeedMinus100`, `SetSpeedZero`, `SetSpeed75`, or `SetSpeed100` from
  the game's currently active `.binds` preset on every invocation, reports the
  resolved preset/file/key, rechecks the foreground game, and sends one
  scan-code key-down/key-up pair with backend and timing evidence;
- `elite-dangerous/supercruise-control` resolves only the dedicated Frontier
  `Supercruise` binding, and the linear
  `elite-dangerous/supercruise-to-destination` workflow requires current
  preflight, Compass, `SUPERCRUISE`, two-frame `SAFE_DISENGAGE_READY`, and
  three-frame visual `STOPPED` evidence around its 75% approach and safe exit;
- `elite-dangerous/supercruise-assist-to-destination` retains that manual
  workflow as an alternative while adding a `DROP` lifecycle owned by the
  in-game Assist computer: it enters Supercruise, visually selects the locked
  target's non-orbit Assist action, requires two
  `SUPERCRUISE_ASSIST_ACTIVE` frames, then normally sends no flight input while
  waiting for the game's automatic drop and three-frame visual stop. Two
  line-of-sight-required frames activate a bounded focus-frame-directed bypass,
  Compass plus visible-target realignment, and fresh Assist ownership Gate;
- `elite-dangerous/leave-station` is the first shipped linear Streaming Action.
  It immediately returns a durable watch URL, asks the supervising model to
  arrange Auto Launch, and requires empty prompt text plus positive `KNOWN`
  visual speed while Mass Lock remains ON before commanding 100% throttle. The
  handover accepts either two strict low-speed frames or four consecutive
  matching low-confidence `0` through `10` OCR frames under the narrower
  workflow-local confidence and margin contract. Its
  events keep observed speed separate from commanded throttle, and it commands
  0% only after the Mass Lock OFF gate, then requires three consecutive
  workflow-local zero-speed OCR confirmations before reporting completion;
- all shipped Rules have no active Monitor or Reaction registrations by
  default; no scheduler or reaction dispatcher is shipped yet.

## Build

Go 1.23 or newer is required. .NET 8 SDK is required only to build the
self-contained ScreenParser and PP-OCR DirectML runtimes; it is not required on
the target Windows machine.

```bash
mkdir -p .build
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
./scripts/build-windows-capture-agent.sh
cp -R Rules .build/
```

`windows-capture-agent.exe` is always the installable GUI-subsystem artifact.
The build script also emits `windows-capture-agent-console.exe` for interactive
terminal diagnostics, `windows-wgc-worker.exe` for the Agent-owned persistent
and crash-isolated WGC runtime, `windows-action-check.exe` for offline Rule
validation,
`windows-action-osd.exe` for the display-only capture, Action, and
Evidence-recording overlay, and the optional `windows-watchdog.exe` and independent
`windows-evidence-recorder.exe` and `windows-visual-log.exe`. It also emits the
Event Stream and all three observation runtimes required by the persistent
installer. It verifies the expected PE subsystem for every emitted executable.

## Deploy from macOS

`scripts/deploy-windows-agent.sh` is the single macOS interface for a complete
binary update. It validates source, builds and hashes all ten deployed
executables, uploads one ZIP over SSH, stops the installed Watchdog and its
currently configured targets, replaces only their binaries, maintains bounded
process-scoped crash dumps for the Agent and WGC worker, then restarts the
Watchdog and waits for its existing target set to become healthy.

It reads the installed Watchdog configuration and Scheduled Task actions as the
only deployment map. It does not ship a Watchdog configuration, register or
change Tasks, choose triggers or restart policy, or start an Evidence or Visual
Log run. After replacement it verifies that every target Task's description,
executable, and complete argument string are byte-for-byte unchanged; this
includes the Visual Log model endpoint.

```bash
./scripts/deploy-windows-agent.sh --host Ronnie-PC
```

`Ronnie-PC` is the default host, so `--host` may be omitted. A dirty worktree is
rejected unless `--allow-dirty` is explicitly supplied. Failed remote staging
is retained for diagnosis.

### Offline Action dependency check

`windows-action-check` is an independent development and release tool. The
capture Agent does not invoke it, load its dependency graph, or validate Rule
dependencies at startup.

Run it against a Rule plugin directory before packaging or publishing:

```bash
go run ./cmd/windows-action-check --rules-dir Rules
go run ./cmd/windows-action-check --rules-dir Rules --json
```

The checker loads Core-owned Action packages, compiles composite and streaming
Starlark entrypoints, and extracts static `action.call`, `action.try_call`, and
`action.on_failure` references. It rejects missing, cross-Rule, streaming-child,
self, dynamic-ID, and cyclic dependencies. Human-readable failures include the
source location and dependency chain. Indirect aliases of the `action` module
or its call primitives are rejected so every runtime dependency remains
statically visible. Exit code `0` means valid, `1` means the report contains
validation issues, and `2` means the check could not run or its report could
not be written. Runtime-specific packages owned outside Core are left to their
own validators.

## Run

Run the diagnostic console build inside the signed-in Windows user's session:

```powershell
.\.build\windows-capture-agent-console.exe `
  --rules-dir (Resolve-Path .\.build\Rules)
```

Available options:

```text
--listen              HTTP listen address (default 0.0.0.0:8787)
--data-dir            artifact and log root
--rules-dir           external Rule plugin directory (default <data-dir>/Rules)
--capture-timeout     per-request timeout (default 5s)
--retention           number of artifacts to retain (default 100)
--log-level           debug, info, warn, or error
--log-file            optional JSON log file
--runtime-log-file    optional Go runtime and fatal stderr log file
--wgc-trace           emit every WGC operation lifecycle at info level
--frontier-bindings-root  Elite Dangerous bindings directory (default under LOCALAPPDATA)
```

The process must not run as a traditional Session 0 Windows service because WGC
requires access to the interactive desktop.

Install the optional Action OSD after the loopback event stream is healthy:

```powershell
.\scripts\install-windows-action-osd.ps1 `
  -ExecutablePath .\.build\windows-action-osd.exe
```

For an explicit event-contract migration, pass `-MinimumEventCursor` with the
last durable cursor owned by the retired contract. The installed OSD skips only
history at or before that boundary; later events remain subject to normal
startup replay and strict validation.

The independent interactive-user task shows a fixed cyan dot for at least
500 ms after the Capture Agent accepts a full or region capture request.
Consecutive captures extend the same pulse; it is an activity disclosure, not
a claim that capture succeeded. It also shows a fixed yellow dot while an
Evidence Recorder holds the session-local
`Local\WindowsAgent.Evidence.Recording.v1` signal; the dot disappears within
one polling interval after the finite run stops or the recorder exits. While an Action is
running, the compact background-free top-left viewfinder additionally shows a
blinking red dot, the short Action name, and at most the latest three explicit
`stream.activity` records. Terminal Action states disappear automatically.
The OSD is excluded from screen capture by default; `-AllowCapture` is intended
only for visual acceptance evidence.

Run the partially landed event-stream service independently on loopback:

```powershell
$tokenBytes = New-Object byte[] 32
$tokenRng = [Security.Cryptography.RandomNumberGenerator]::Create()
$tokenRng.GetBytes($tokenBytes)
[IO.File]::WriteAllText(
  (Join-Path $PWD "event-stream.token"),
  [Convert]::ToBase64String($tokenBytes)
)
.\.build\windows-event-stream.exe `
  --listen 127.0.0.1:8788 `
  --data-dir (Join-Path $PWD "event-data") `
  --token-file (Join-Path $PWD "event-stream.token") `
  --log-file (Join-Path $PWD "event-stream.jsonl")
```

Run the partially landed Elite Dangerous visual log as its own process after
the Evidence recorder and event stream are healthy. Both processes may remain
idle; before starting a Visual Log run, explicitly start a finite Evidence run
and wait for its state to become `recording`. The model key file is local
operator configuration and must not be stored in the Rule:

```powershell
.\.build\windows-visual-log.exe `
  --config (Resolve-Path .\Rules\EliteDangerous64.exe\VisualLog\config.json) `
  --model-base-url http://<oMLX-LAN-IP>:8001/v1 `
  --model-api-key-file (Resolve-Path .\omlx-api.key) `
  --event-base-url http://127.0.0.1:8788 `
  --event-token-file (Resolve-Path .\event-stream.token) `
  --control-listen 127.0.0.1:8789 `
  --control-token-file (Resolve-Path .\visual-log-control.token) `
  --log-file (Join-Path $PWD "visual-log.jsonl") `
  --status-file (Join-Path $PWD "visual-log-status.json")
```

The independent process starts idle. A high-level model may request and stop
one logging run through its authenticated loopback control interface:

```text
GET    http://127.0.0.1:8789/v1/visual-log/status
POST   http://127.0.0.1:8789/v1/visual-log/runs       body: {}
DELETE http://127.0.0.1:8789/v1/visual-log/runs/current
```

Starting a run creates a new producer session and performs the configured
warm-up before the process-owned description loop becomes active. Stopping the
run leaves the independent process idle and has no path to the evidence layer.

Run the evidence recorder as a separate on-demand PC process. The process stays
idle until an authenticated finite recording run is accepted. The token is
local operator configuration and the data directory contains private video:

```powershell
.\.build\windows-evidence-recorder.exe `
  --config (Resolve-Path .\Rules\EliteDangerous64.exe\Evidence\config.json) `
  --listen 127.0.0.1:8792 `
  --data-dir (Join-Path $PWD "evidence-data") `
  --token-file (Resolve-Path .\evidence.token)
```

Process startup does not open WGC or show the recording indicator. A finite run
obtains Windows borderless-capture consent, owns one persistent WGC session
with `IsBorderRequired=false`, samples its newest frame at 1 FPS, and records
1080p H.264 MP4 segments. Denied or unsupported borderless access fails that
run; it never silently records with the Windows capture border still visible.
Each second is a video sample or an explicit gap; Visual Log and Gemma failures
do not terminate the run. Its loopback API is:

```text
GET http://127.0.0.1:8792/healthz
GET http://127.0.0.1:8792/v1/evidence/status
POST http://127.0.0.1:8792/v1/evidence/runs
GET http://127.0.0.1:8792/v1/evidence/runs/<runId>
GET http://127.0.0.1:8792/v1/evidence/range?from=<UTC>&to=<UTC>
POST http://127.0.0.1:8792/v1/evidence/contact-sheet
```

Start a default 20-minute run with `{}`, or request a different duration with
strict JSON such as `{"durationSeconds":300}`. `durationSeconds` is optional
but, when present, must be an integer from 1 through 3600. Twenty minutes is
the default; one hour is the hard maximum. A successful start returns HTTP 202 with
`finite:true`, `runId`, `durationSeconds`, `requestedAt`, and `endsAt`; the
deadline starts when the request is accepted. State advances from `starting`
to `recording` only after WGC and the recording indicator have started, then to
`completed` after the deadline and final segment commit. There is no extension,
manual stop, pause, or delete route. Starting while another run is active
returns HTTP 409 `EVIDENCE_RUN_ACTIVE` with that run's finite deadline.

Every evidence route except health requires the Evidence Bearer token. A
successful half-open UTC
range returns a ZIP with `manifest.json`, explicit committed gaps,
`missingSlots` for recorder downtime, and integrity-checked overlapping MP4
segments. Visual Log reads only the configured PC-local frame tap; it cannot
implicitly start or extend Evidence, or download individual Evidence frames
over HTTP.

The authenticated contact-sheet route accepts strict JSON containing `from`,
`columns`, `rows`, and `intervalSeconds`. The PC decodes exact timestamps from
committed Evidence MP4 segments and returns one timestamped JPEG grid. It never
captures the screen again or substitutes a nearby frame. Explicit Evidence
gaps and missing slots appear as labelled cells. The grid is a bandwidth-light
locator; retrieve the selected MP4 range before making an authoritative claim.

The persistent installer launches the event service as an independent,
interactive-user Scheduled Task. It creates the token only when absent and
rejects an existing malformed token instead of replacing it. Append, replay,
and NDJSON live-stream requests require the exact token; `/healthz` is the only
unauthenticated route.

Install the Evidence Recorder and Visual Log control processes independently.
The installer creates independent interactive-user Tasks without their own
trigger or restart policy and starts each resident control service for health
acceptance. The Watchdog keeps those processes available; service availability
does not start an Evidence recording or Visual Log inference run:

```powershell
.\scripts\install-windows-observation-processes.ps1 `
  -EvidenceExecutablePath .\.build\windows-evidence-recorder.exe `
  -VisualLogExecutablePath .\.build\windows-visual-log.exe `
  -VisualLogModelBaseURL http://model-host:8001/v1
```

The first installation requires `VisualLogModelBaseURL` and verifies
`/models` from the Windows host before changing either Task. Later
reinstallations preserve the URL from the owned Visual Log Task when the
parameter is omitted. Changing an installed endpoint requires both an explicit
new value and `-AllowVisualLogModelBaseURLChange`; the replacement endpoint is
verified before the resident processes are stopped.

Add exact `evidence-recorder` and `visual-log` targets to the Watchdog
configuration. The executables remain independent processes and expose
authenticated run-control APIs, while the Watchdog owns only process
availability. Neither process starts a run as a side effect of installation or
recovery.

After all module installers have created their watchdog-managed Tasks, author
an exact local configuration containing all five targets and install the
external Watchdog:

```powershell
.\scripts\install-windows-watchdog.ps1 `
  -ExecutablePath .\.build\windows-watchdog.exe `
  -ConfigPath .\watchdog-config.json
```

The watchdog has [one-way coupling and no automatic self-recovery](docs/design/windows-watchdog.md).
Monitored modules do not register with or depend on it. Its AtLogOn Scheduled
Task has a zero restart count; if the watchdog crashes, other modules continue
and the watchdog remains stopped for explicit operator diagnosis. It is the
AtLogOn entrypoint for watchdog-managed resident processes and bootstraps their
Tasks in the dependency order declared by its own configuration.

Follow the installed stream from macOS through an SSH tunnel without exposing
the loopback-only event API on the Windows network interface:

```bash
./scripts/watch-windows-event-stream.sh \
  --ssh-host user@Windows-PC
```

The watcher retrieves the installed token through the same SSH connection,
replays the latest 10 events, and then follows new NDJSON records until
`Control-C`. Use `--tail 0` to follow only newly committed events or `--after`
to provide an exact durable cursor. Connection messages go to stderr; stdout
contains only event records and can be piped to another reader.

Create one strict launcher request outside the Rule plugin:

```json
{
  "inputs": {}
}
```

Run the registered capability through the generic launcher from the signed-in
session:

```powershell
.\.build\windows-observation-job.exe `
  --capability crimson-desert/inventory `
  --install-root (Resolve-Path .\.build) `
  --rules-dir (Resolve-Path .\.build\Rules) `
  --request-file (Resolve-Path .\inventory-request.json)
```

`inputSchema` belongs to the Script Package. File roots are also package
declarations: the Host resolves only supported Windows known folders and never
accepts absolute roots from the caller. The inventory Starlark owns its bounded
account, slot, and newest-save selection. The launcher derives the expected
foreground executable from the capability's owning Rule folder. The
`--process-id` and `--process-path` flags exist only for a trusted local host
that already resolved that same owning-Rule process; the observer still
revalidates its path, creation time, and executable SHA-256.

The package declares native-library alias `save-decoder`, its
`windows-amd64` artifact, call limit, and native-memory limit. Starlark
loads only that alias through `native.load_library("save-decoder")`; it owns the
crimson-rs export signatures, record layout, return codes, and JSON conversion.
`load_library` is used because `load` is a reserved Starlark keyword.

## Persistent watchdog-managed startup

From the repository root in PowerShell:

```powershell
.\scripts\install-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe `
  -RulesPath .\.build\Rules `
  -OCRRuntimeBundlePath .\.build\ppocr-w480-bundle
```

The installer copies the capture executable, generic Starlark launcher,
Script Runner, Observer, event-stream executable, external Rule plugins, and
any Rule-declared resident runtime bundle under the current user's
`%LOCALAPPDATA%`. By default it registers separate interactive-token on-demand
Scheduled Tasks for capture and event streaming with no triggers and zero
restart count, starts them once for installation acceptance, and verifies both
`/healthz` endpoints. The Watchdog becomes their only persistent AtLogOn
launcher. All five executables must
be present beside the selected capture build artifact before installation.
The installer does not create an SCM service or modify Windows Firewall.
It validates that both persistent executables use PE subsystem `Windows GUI`
before stopping any existing task. A console build is rejected because Task
Scheduler's `Hidden` setting cannot suppress its console window.

For an explicit development environment without the Watchdog, request the
standalone task policy rather than relying on an automatic compatibility path:

```powershell
.\scripts\install-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe `
  -RulesPath .\.build\Rules `
  -OCRRuntimeBundlePath .\.build\ppocr-w480-bundle `
  -StartupMode Standalone
```

The Action OSD installer follows the same explicit `WatchdogManaged` default
and `Standalone` override.

The persistent installation enables bounded crash diagnostics for the capture
process and its WGC worker. Structured WGC lifecycle records are written to
`logs/agent.jsonl`; Go runtime and fatal stderr output is appended to
`logs/runtime-stderr.log`. The current user's Windows Error Reporting
`LocalDumps` entries are scoped to `windows-capture-agent.exe` and
`windows-wgc-worker.exe`; each retains at most five full dumps under `dumps/`.
These dumps can contain private process memory and must never be published or
committed. Pass `-WGCTrace $false` when reinstalling to keep only retry and
failure records after an incident has been bounded.

For a code-only update of an existing installation, use the transactional
updater. It checks the GUI subsystem and SHA-256 before stopping the task,
keeps prior Agent and worker binaries as timestamped backups when present,
verifies the interactive listener and `/healthz`, and restores the complete
previous binary set if the new process fails:

```powershell
.\scripts\update-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe
```

Builds that stored `rule.agents.sha256`, or matched Rule metadata without
`rule.scripts`, `rule.actions`, `rule.registrations`, or `rule.runtimes`, use an
incompatible capture metadata contract. The installer detects those captures
before stopping the current task and refuses the migration unless explicitly
asked to preserve them:

```powershell
.\scripts\install-windows-capture-agent.ps1 `
  -ExecutablePath .\.build\windows-capture-agent.exe `
  -RulesPath .\.build\Rules `
  -OCRRuntimeBundlePath .\.build\ppocr-w480-bundle `
  -ArchiveIncompatibleCaptures
```

The switch renames the existing `captures` directory to a timestamped
`captures.pre-external-rules-*` archive. It does not reinterpret or delete the
old artifacts.

Build the self-contained Windows runtime bundle:

```bash
python3 tools/screenparser-runtime/publish.py \
  --dotnet "$(command -v dotnet)" \
  --output-dir "$PWD/.build/screenparser-directml"
```

Prepare the official PP-OCRv6 small detection and recognition ONNX artifacts,
generate the character dictionary, and specialize recognition to the reviewed
text-line width. The output directory must be empty:

```bash
python3 -m pip install -r tools/ppocr-model/requirements-build.in
python3 tools/ppocr-model/prepare.py \
  --output-dir "$PWD/.build/ppocrv6-small-w480" \
  --recognition-input-width 480
python3 tools/ppocr-runtime/publish.py \
  --dotnet "$(command -v dotnet)" \
  --output-dir "$PWD/.build/ppocr-directml"
```

The PP-OCR executable implements two separately declared framed pipelines:
aspect-preserved, right-padded text-line recognition and region detection plus
w480 recognition. Recognition requests explicitly choose unrestricted or
digit-only CTC decoding; digit-only responses retain the unrestricted candidate
and confidence margin as evidence.
The latter returns quadrilateral boxes rather than game state. Both disable
ONNX Runtime CPU-provider fallback and validate pinned artifacts exactly.
WindowsAgent starts either worker only while the owning Rule is active, as
declared by `runtimeProfiles`; residency is not a Monitor and emits no event.
The developer benchmark tool remains a separate bounded diagnostic.

For bounded precision or provider diagnostics, publish the separate one-shot
console tool. It reads one hash-pinned RGB24 frame, performs a bounded number
of DirectML inferences, prints one JSON result, and never captures the desktop,
starts a loop, or appends to the event stream:

```bash
dotnet publish \
  tools/screenparser-directml-one-shot/ScreenParser.DirectML.OneShot/ScreenParser.DirectML.OneShot.csproj \
  --configuration Release \
  --runtime win-x64 \
  --self-contained true \
  -p:PublishSingleFile=true \
  -p:IncludeNativeLibrariesForSelfExtract=true \
  --output "$PWD/.build/screenparser-directml-one-shot"
```

Run it only with an absolute strict-JSON diagnostic spec. The spec pins the
model and RGB24 frame by SHA-256, declares the frame dimensions, model I/O,
labels, precision, thresholds, and a maximum of three warmups and ten measured
runs:

```powershell
.\ScreenParser.DirectML.OneShot.exe --spec C:\absolute\path\to\one-shot.json
```

This tool accepts `fp32`, `fp16`, and `int8` only for isolated measurement. It
does not widen the production Action manifest, installer, or runtime contract.

The pinned `.pt` checkpoint is a build-time input only. The ONNX exporter
requires at least one real validation image and emits both the model and its
verified `artifact.json`:

```bash
python3 tools/screenparser-model/export_onnx.py \
  --source-model /absolute/path/to/best.pt \
  --validation-image /absolute/path/to/real-screen.png \
  --output-dir /absolute/empty/output
```

Install the finite ScreenParser Action with the ONNX artifact declared by
`Rules/Palworld-Win64-Shipping.exe/Actions/screenparser/manifest.json` and the published runtime
bundle:

```powershell
.\scripts\install-windows-screenparser.ps1 `
  -RulePath .\Rules\Palworld-Win64-Shipping.exe `
  -ModelPath C:\absolute\path\to\screenparser-v2-f029e565-opset20-fp16-1280.onnx `
  -RuntimeBundlePath C:\absolute\path\to\screenparser-directml
```

The installer verifies both artifact manifests and SHA-256 values, installs one
shared runtime/model copy, and creates no task or background process. It removes
only the exact owned legacy ScreenParser loop and scene-reducer tasks. It
installs no Python, PyTorch, CUDA Toolkit, or .NET SDK. A trusted VLM host invokes
the installed runtime with `--request`, `--frame-root`, and `--response`; each
invocation processes one exact frame, writes no streaming event, and exits.

Publish one updated Rule plugin without rebuilding the executable or restarting
the task:

```powershell
.\scripts\sync-windows-agent-rule.ps1 `
  -SourceRulePath .\Rules\CrimsonDesert.exe `
  -DestinationRulesDir "$env:LOCALAPPDATA\gameGuide\windows-capture-agent\Rules"
```

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
GET  /v1/rules/{rule-id}/scripts
GET  /v3/rules/{rule-id}/actions
GET  /v3/rules/{rule-id}/registrations
GET  /v3/rules/{rule-id}/action-sequence-tool
GET  /v4/rules/{rule-id}/runtimes
POST /v1/scripts/run
POST /v1/actions/invoke
POST /v1/action-sequences/invoke
GET  /v1/action-invocations/{invocation-id}
GET  /v1/action-invocations/{invocation-id}/events?after={cursor}
POST /v1/action-invocations/{invocation-id}/stop
```

Create a capture:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"include_cursor":true}' `
  http://127.0.0.1:8787/v1/captures
```

Omitting `profile` selects `native-jpeg`. The complete supported request
profiles are `native-jpeg` (Q90, 4:4:4), `1080p-jpeg` (fit inside 1920x1080,
Q90, 4:4:4), and `native-png` (lossless, PNG BestSpeed). Unknown profiles and
encoding failures are returned explicitly; the agent does not change formats
or fall back to PNG.

Download the latest image using the extension reported by its metadata:

```powershell
curl.exe `
  -o capture.jpg `
  http://127.0.0.1:8787/v1/captures/latest/content
```

Discover the current Script contracts for a matched Rule:

```powershell
curl.exe http://127.0.0.1:8787/v1/rules/CrimsonDesert.exe/scripts
```

The catalog returns each capability's ID, declared runtime, title, package
version, input schema, output schema, and launcher endpoint. It
is read-only and does not execute a Script.

Run one registered Script from the signed-in agent session:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary "@inventory-invocation.json" `
  http://127.0.0.1:8787/v1/scripts/run
```

The invocation body contains only `capability` and package-defined `inputs`;
Host filesystem roots are never caller input. No bearer token or other HTTP
credential is required. Script execution is serialized and does not upload,
rewrite, or reload a Rule plugin.

Invoke any Action through the unified surface:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"actionId":"elite-dangerous/ship-status","inputs":{}}' `
  http://127.0.0.1:8787/v1/actions/invoke
```

Use `"actionId":"elite-dangerous/ship-speed"` on the same endpoint to read
visual speed evidence. `MOVING` makes the concrete `speed.displayValue`
available, while `LOW_SPEED` deliberately withholds the unreliable exact
single digit and retains it only as `rawCandidate`. `UNKNOWN` is a valid
observation and must not be replaced with the last requested throttle setting.

A finite Action returns HTTP `200`, `state: COMPLETED`, and `output`. A
streaming Action returns HTTP `202`, `state: RUNNING`, and a `watch` object.
Follow its returned URL with `curl.exe -N`; the NDJSON connection replays the
durable invocation events and closes when the Action completes, fails, or is
cancelled. The `stop` object appears only when that Action explicitly declares
itself interruptible.

For a disposable multi-Action plan, first fetch the strict model tool schema
from `/v3/rules/{rule-id}/action-sequence-tool`, then submit its arguments to
`POST /v1/action-sequences/invoke`. The response is HTTP `202` and uses the
same watch, status, and stop endpoints. All steps are validated before the
first Action runs; child outputs and streaming events are forwarded on one
parent correlation chain with step, Action, and child-execution provenance.
The Action OSD displays the active child Action, `Step n/total`, and wrapped
child activity while keeping the Sequence as the only display session.

Start the supervised Elite Dangerous departure only after the higher model has
confirmed the ship is inside a station:

```powershell
curl.exe `
  -H "Content-Type: application/json" `
  --data-binary '{"actionId":"elite-dangerous/leave-station","inputs":{"stationConfirmed":true}}' `
  http://127.0.0.1:8787/v1/actions/invoke
```

The initial stream event is `AWAITING_AUTO_LAUNCH`. During that phase the
supervising model captures the screen and invokes `elite-dangerous/ui-control`
one logical key at a time. The Streaming Action does not guess a fixed Auto
Launch key sequence. Once the prompt pipeline observes Auto Launch, the
workflow requires a `MOVING` observation, five samples without a classified
Auto Launch prompt, Mass Lock ON, and two `STOPPED` or `LOW_SPEED` observations. It then
continues autonomously through the 100% command and Mass Lock OFF gates. After
the 0% command it enters `VERIFYING_STOP`; three consecutive current frames
must be classified `STOPPED` by the dedicated slashed-zero pixel topology
before `COMPLETED`. This final phase calls only the resident speed path and marks flight prompt and Mass Lock as
unobserved instead of repeating their slower pipelines or retaining stale
values. Stream fields named `observedSpeed*` are visual evidence;
`commandedThrottle` is input-command state, and inability to confirm the stop
fails explicitly.

Only one capture can run at a time. A concurrent request receives
`409 capture_busy`. Each completed artifact contains `capture.jpg` or
`capture.png` plus `metadata.json`. Metadata records `profile`, `format`,
`content_type`, and, for JPEG, `quality` and `chroma_subsampling`. The response
and metadata also include a required `foreground`
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
    "description": "The executing agent must read the Rule navigation documents before taking any rule-specific action.",
    "id": "Game.exe",
    "agents": {
      "url": "/v1/rules/Game.exe/AGENTS.md",
      "content_type": "text/markdown; charset=utf-8"
    },
    "scripts": {
      "url": "/v1/rules/Game.exe/scripts",
      "content_type": "application/json; charset=utf-8"
    },
    "actions": {
      "url": "/v3/rules/Game.exe/actions",
      "content_type": "application/json; charset=utf-8"
    },
    "registrations": {
      "url": "/v3/rules/Game.exe/registrations",
      "content_type": "application/json; charset=utf-8"
    }
  }
}
```

The foreground window is sampled immediately after WGC produces the captured
frame. The same response resolves its executable name against the current
external folders under `Rules/`; this keeps the capture JSON as Codex's single
Windows perception entry point. Each request reloads `rule.json` and
`AGENTS.md`, so a completed Rule plugin replacement requires no agent reload or
task restart. Codex follows `rule.agents.url` for policy,
`rule.actions.url` for executable capabilities,
`rule.registrations.url` for explicitly configured Monitor and Reaction
instances, and `rule.scripts.url` for the current observation compatibility
projection. Script package validation occurs only when that catalog
or capability is requested; capture remains independent from Script package
health. An executable without a rule reports
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
cmd/windows-observation-job/     generic local windows-observation-v1 launcher
cmd/windows-observation-script-runner/ isolated Starlark runner
cmd/windows-observer/            unified read-only memory/file observer
cmd/windows-event-stream/        authenticated local event journal service
cmd/windows-visual-log/          optional independent oMLX scene-description producer
cmd/windows-watchdog/            external one-way process observer and recovery
cmd/windows-screen-scene-reducer/ retired raw-screen reducer reference
docs/design/                     maintained design registry
docs/protocol/                   runtime protocol usage
docs/testing/                    external black-box acceptance contracts
internal/observationjob/         finite broker and Windows Job Object limits
internal/observationlauncher/    native child-process isolation
internal/observer/               permission-bounded memory/file backends
internal/scriptrunner/           Starlark runtime and generic Windows native FFI
internal/artifact/               artifact transactions and retention
internal/capture/                screenshot capability contracts
internal/captureindicator/       session-local recent-capture activity signal
internal/config/                 process configuration
internal/actionrun/              finite and streaming invocation lifecycle
internal/actionsequence/         bounded ephemeral sequence and strict model schema
internal/actioncheck/            offline Action package and dependency validation
internal/eventclient/            authenticated Agent-to-journal client
internal/eventhttp/              authenticated event append/replay HTTP API
internal/eventstream/            strict durable event journal
internal/evidence/               finite recording lifecycle, authoritative video store, range archive, and contact sheets
internal/evidencehttp/           authenticated Evidence run-control and read interface
internal/mfvideo/                native Media Foundation Evidence encoder and decoder
internal/recordingindicator/      session-local Evidence recording-presence signal
internal/visuallog/              strict Game config, evidence/model adapters, and producer loop
internal/visualloghttp/          authenticated loopback visual-log control adapter
internal/watchdog/               target probes, bounded recovery, atomic status
internal/scenereducer/            cursor, scene delta, and append recovery
internal/foreground/             foreground process observation
internal/httpapi/                current HTTP surface
internal/pixels/                 SDR and HDR pixel conversion
internal/rules/                  live Rule plugin loading and navigation
internal/scriptlaunch/           strict generic launcher request contract
internal/streamaction/            bounded streaming Starlark orchestration runtime
internal/wgc/                    Request and persistent WGC / Direct3D 11 implementations
internal/wgcworker/              Versioned worker protocol and Agent-side generation owner
Rules/<Executable.exe>/          distributable Rule v6 runtimes, Actions, registrations, and guidance
runtimes/screenparser-directml/   finite self-contained DirectML Action runtime
runtimes/ppocr-directml/          resident PP-OCR text-line and text-regions workers
tools/screenparser-model/         build-only pinned .pt to verified ONNX exporter
tools/screenparser-runtime/       reproducible Windows runtime publisher
tools/ppocr-model/                official PP-OCR artifacts and shape specialization
tools/ppocr-runtime/              PP-OCR publisher and bounded benchmark tool
scripts/                         Windows installation helpers
```

New or changed Script packages must follow the
[`Script Package development contract`](docs/script-development-contract.md).
It defines package ownership, source-transition rules, manifest validation,
native ABI responsibility, privacy boundaries, and required validation.

OpenCode model validation must follow the
[`OpenCode black-box acceptance contract`](docs/testing/opencode-black-box-acceptance-contract.md).
It evaluates only externally visible OpenCode inputs, tool events, request
counts, and final-answer consistency.

New capabilities should receive their own internal package and API contract
instead of being folded into the screenshot packages.

## Security and contributions

See [SECURITY.md](SECURITY.md) before exposing the listener or reporting a
vulnerability. Contributions are described in
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

WindowsAgent is available under the [MIT License](LICENSE).
