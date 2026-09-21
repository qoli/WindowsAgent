# Assist GUI and executable release distribution

## Current Status

Partially landed.

The repository now has one strict multi-process release catalog, deterministic
SHA-256 output, a tag-driven GitHub Release workflow, a framework-dependent C#
WinUI 3 AssistGUI payload, its Go `windows-assist-backend.exe`, and an
independent `windows-tailscale-adapter.exe`. The GUI reports local Agent
health, private-LAN endpoints, and TailscaleAdapter state. It can start an
ephemeral adapter from a one-off auth key and request a graceful logout through
a local stop file.

The WinUI frontend communicates with exactly one sibling backend process using
the strict `windows-assist-v1` JSON-lines interface. The backend can download
the latest public catalog and checksum list over an HTTP/1.1-only client, stage
and verify the repository-defined installable executable set, and hand the
transaction to a verified staged backend copy running elevated. Install,
Update, and Repair invoke the repository-owned Capture Agent and Watchdog
PowerShell installers with setup-owned inputs; Uninstall removes their owned
Scheduled Tasks and runtime executables while preserving user data. The
transaction preserves owned task definitions and installed files for rollback,
then requires existing installer health and process-owner read-back before it
reports success.

## Problem

WindowsAgent is a multi-process system, but its build, local SSH deployment,
and public delivery previously used different executable inventories. A new PC
also required operator-driven scripts before it could expose useful LAN or
private-overlay access information.

The public delivery contract needs to publish each executable independently,
plus one catalog and one SHA-256 list. It must not hide the process model in a
traditional Setup package or a bundled executable archive.

## Scope

This design owns:

- the public Windows executable catalog and artifact classes;
- the GitHub Release asset set;
- the AssistGUI bootstrap, information, and settings surface;
- the private WinUI-to-Go JSON-lines setup interface;
- verified HTTP/1.1 release downloads;
- the optional ephemeral Tailscale transport process; and
- the whole-release Install, Update, Repair, and Uninstall transaction; and
- explicit Start and Stop operations through the installed Watchdog contract.

It does not redefine Capture Agent, Event Stream, delegated Pi, Rule, Action,
Watchdog, or inference-runtime semantics. It does not make Tailscale mandatory
and does not silently select it when LAN access fails.

## Release model

`internal/releasecatalog.Specs` is the executable-set authority. Each artifact
has one stable name, role, class, expected Windows PE subsystem, byte count, and
SHA-256 digest. The current classes are:

- `bootstrap` for the WinUI AssistGUI and Go Assist backend;
- `runtime-required` for the minimum Agent process graph;
- `runtime-optional` for independently enabled companion processes;
- `operator-tool` for clients and offline tools; and
- `diagnostic` for binaries that must not replace installed GUI artifacts.

The generator fails on missing or unexpected executables, duplicate names,
unsafe paths, metadata drift, invalid PE subsystem, or noncanonical digest.
GitHub Release uploads the individual `.exe` files together with
`windowsagent-release.json` and `SHA256SUMS`.

The release additionally provides `windows-assist-gui.zip` as a narrow
bootstrap transport. That archive contains the complete framework-dependent
WinUI publish output at its root together with the sibling
`windows-assist-backend.exe`. It does not duplicate the release catalog,
checksums, or any installed WindowsAgent runtime executable, and it does not
hide the multi-process release inside a combined archive. The individually
published `windows-assist-gui.exe` remains part of the executable catalog for
identity and PE verification, but the ZIP is the runnable AssistGUI
distribution because the apphost requires its adjacent managed payload files.

AssistGUI keeps `WindowsPackageType=None` and uses the Windows App SDK's
default bootstrap auto-initialization. It does not call the bootstrap API,
override the default `OnNoMatch_ShowUI` option, inspect installed runtime
packages, show a custom prerequisite MessageBox, maintain runtime download
URLs, or add a native bootstrapper. A missing .NET Desktop Runtime remains the
.NET GUI apphost's official missing-framework UX. When .NET is present but a
matching Windows App Runtime is not, the default Windows App SDK bootstrap
acquisition UI owns that result.

The framework-dependent publish must also carry the generated
`windows-assist-gui.pri`. Current Windows App SDK publish targets generate that
PRI in the build output but omit it from unpackaged publish output; the project
wires the generated PRI into `ResolvedFileToPublish`, and the build script
fails if it is absent. This is publish completeness glue, not prerequisite
detection or a second bootstrap path.

The catalog is a coherent release set. AssistGUI must stage and validate the
complete selected set before mutating an installation; it must not report a
partially updated process graph as a successful release update.

An Assist bootstrap may operate against a runtime installed by another release
generation. Download and staging continue to require the current catalog's
complete executable set. When starting an already-installed executable,
AssistGUI instead validates the installed catalog's schema and structure, then
requires the selected executable's name, role, class, subsystem, byte count,
and digest to match its current contract. Adding an unrelated release artifact
must not prevent a newer bootstrap from starting a verified older
TailscaleAdapter.

Bootstrap artifacts are distribution processes, not installed WindowsAgent
runtime artifacts. Install, Update, and Repair deploy only `runtime-required`
and `runtime-optional` executables. The currently running backend copies itself
to the private transaction stage for the elevated handoff; the framework-
dependent GUI payload remains in the user-extracted bootstrap directory.

## AssistGUI setup surface

The Microsoft WinUI 3 Gallery `SettingsPage.xaml` at commit
`abb8cb4cef04a5080f5c0396f67a7ec502b36179` remains a visual reference, not the
product or runtime authority. AssistGUI uses a Mica window, TitleBar, section
typography, single-column scrolling content, grouped setting rows, and a
641-effective-pixel responsive boundary. It does not adopt the Gallery's
NavigationView, search, sample catalog, automation helpers, multiple page
destinations, experimental Windows App SDK version, or .NET target. Native
WinUI Borders, Grids, MenuFlyouts, Buttons, CheckBox, and PasswordBox implement
the compact surface; no SettingsControls package is required.

The persistent page has exactly three sections: Maintenance, Status, and
Access. Maintenance contains one state-appropriate Install button or a menu
with Update, Reinstall, and Uninstall, plus the installed version. Reinstall is
the user-facing label for the existing backend `repair` command and does not
define another installation transaction. Status reduces the concrete Capture
Agent and Watchdog facts to one WindowsAgent Running or Stopped value with a
state-appropriate Start or Stop menu action, plus a start-at-sign-in CheckBox.
Access groups the LAN endpoint and Tailscale controls. The Tailscale action area
switches between auth-key plus Connect while inactive and Disconnect while
`STARTING`, `ONLINE`, or `STOPPING`; status and assigned addresses remain in
the same row.

The Status section header owns the explicit Refresh action; page commands do
not compete with window chrome in the TitleBar. Initial inspection and manual
Refresh update the snapshot silently: they show neither the operation overlay
nor a success InfoBar, while inspection failures remain explicit. Mutating
backend requests present progress events in a blocking smoke-layer operation
overlay rather than a persistent Log section. The overlay has no dismiss
action, prevents concurrent page input, and closes automatically on a terminal
result. Success or error is then reported through the page InfoBar, so failures
do not disappear with the transient progress surface. Implementation details
such as the sibling backend executable are not user-facing content.

AssistGUI also writes one `windows-assist-gui.log` JSON-lines file beside its
executable. After single-instance ownership is established, every real GUI
launch truncates that file before recording the current session's commands,
backend progress, concrete state summaries, terminal results, cancellations,
and errors. Records are flushed as they are written. Tailscale auth-key bytes
never enter the log; only whether a key was supplied may be recorded.

TailscaleAdapter is disabled by default. AssistGUI always reports Agent health
and active private-LAN IPv4 endpoints. When the adapter is enabled it also
reports `STARTING`, `ONLINE`, `STOPPING`, or `FAILED`, plus assigned Tailscale
IPv4 and IPv6 addresses when available.

The auth key is accepted through a password edit control, passed to the adapter
on standard input, cleared from the control after process start, and never
placed in command-line arguments or status JSON.

AssistGUI has no capability selector or optional-capability installation
model. Install, Update, and Repair stage every executable that the release
catalog marks as an installable WindowsAgent runtime artifact. Release classes
remain artifact metadata; they are not presented as product choices. Operator
tools and the console diagnostic remain release assets but are not installed
runtime processes.

The GUI exposes the concrete operations Install, Update, Reinstall, Uninstall,
Start WindowsAgent, and Stop WindowsAgent. Reinstall maps to the existing
backend Repair command. It reports Not installed or WindowsAgent
Running/Stopped, installed version, start-at-sign-in state, LAN endpoints, and
Tailscale status and assigned IPs. Capture Agent and Watchdog remain concrete
backend snapshot facts but are not presented as separate product concepts.
A staged backend copy waits for both the original GUI and original backend to
exit before applying a mutation, so setup never overwrites running
executables.

Start verifies the installed Watchdog and configured target ownership, starts
only the Watchdog Scheduled Task, and waits for the existing Watchdog status to
report every configured target healthy. It does not define a second target
startup order. Stop verifies the same ownership, stops the Watchdog before its
targets, stops only exact installed executable paths or owned Scheduled Tasks,
requests graceful Tailscale logout, and requires a final process read-back. It
preserves executable files, Scheduled Task definitions, start-at-sign-in
configuration, Rules, journals, and user data. Stop and Uninstall remain
distinct operations.

When Start receives no auth key, TailscaleAdapter remains disabled. When Start
receives a non-empty auth key, it first establishes Watchdog health and then
starts the adapter. Core WindowsAgent startup and Tailscale enrollment retain
separate concrete results.

The elevated setup mutation is restricted to the current user's fixed WindowsAgent
data directory and the repository-owned Capture Agent, Event Stream, and
Watchdog Scheduled Tasks. AssistGUI embeds and invokes the existing
`install-windows-capture-agent.ps1`, `sync-windows-agent-rule.ps1`, and
`install-windows-watchdog.ps1` sources. It generates only the concrete glue
inputs those installers require for an unfamiliar machine: a canonical data
directory, an initially empty Rules directory, and a two-target Watchdog
configuration for Event Stream then Capture Agent. Existing Rules and user
data are preserved. An existing task with the same name but a different
ownership description is a terminal error. Rollback restores task XML,
executables, release metadata, and Watchdog configuration.

Watchdog is installed and started as part of setup. The user-facing checkbox
controls whether its Scheduled Task has an at-sign-in trigger; it does not
create a second lifecycle mechanism. TailscaleAdapter is always deployed with
the runtime, remains stopped when the auth-key field is empty, and starts only
after a non-empty one-off key is supplied.

## WinUI/backend interface

The frontend starts the sibling backend without command-line arguments, writes
exactly one UTF-8 JSON request to standard input, closes input, and consumes a
bounded sequence of strict JSON-lines events. Every request and event carries
`protocol: windows-assist-v1`, a UUID request ID, and monotonic event sequence.
Commands are `inspect`, `install`, `update`, `repair`, `uninstall`, `start`,
`stop`, `configure-watchdog`, `start-tailscale`, and `stop-tailscale`.
Progress, concrete snapshot, terminal result, and terminal error are the only
event types. A Tailscale auth key may appear only in the one stdin request; it
must never appear in arguments, events, logs, receipts, or status files.

The backend is one process per request, not another resident WindowsAgent
daemon. The WinUI frontend contains no release selection, Scheduled Task,
installer, rollback, Watchdog target, or Tailscale process implementation.

## TailscaleAdapter model

The adapter is an independent self-contained Go module using `tsnet`. It owns
one ephemeral node and a tailnet-only TCP listener that forwards to an explicit
loopback WindowsAgent address. It never changes OS routing or DNS and accepts
only a loopback forwarding target.

The adapter writes a strict local status JSON and performs explicit logout on a
signal or local stop request. Missing auth input, failed enrollment, missing
assigned IP, listener failure, status-write failure, or logout failure is a
terminal error. It does not start SSH, RDP, Bitvise, or another transport.

## Transport requirements

Assist backend release downloads use a dedicated HTTP/1.1-only `net/http` transport.
HTTP/2 negotiation is disabled and no HTTP/3 client is introduced. Downloaded
bytes, byte count, digest, and PE subsystem must all match the catalog before a
file can enter staging.

`tsnet` owns its control-plane and DERP transports. WindowsAgent does not alter
their protocol selection or silently replace the provider.

## Relationship to other designs

The Capture Agent remains the signed-in interactive-session host. Event Stream,
Watchdog, delegated Pi, and other companions retain their independent process
and control-plane contracts. This design publishes and installs those binaries;
it does not absorb their implementations into AssistGUI.

## Prerequisite acceptance

The framework-dependent bootstrap behavior was accepted on 2026-09-19 in
disposable, network-disconnected Windows 11 ARM64 VMs running the published
x64 payload through Windows emulation:

- On a fresh installation with no .NET Desktop Runtime, launching
  `windows-assist-gui.exe` displayed the official .NET GUI apphost prompt,
  including **You must install .NET Desktop Runtime to run this application**
  and the framework-owned **Download it now** action.
- After installing only .NET 8 Desktop Runtime 8.0.31 x64, with no matching
  Windows App Runtime present, the same payload reached Windows App SDK
  bootstrap auto-initialization and displayed its official acquisition prompt
  for Windows App Runtime 2.x with MSIX package version 2.4.0 or newer.

Both observations used the same complete framework-dependent publish payload,
including `windows-assist-gui.pri`, and neither environment had network access
that could silently satisfy an acquisition request. No repository-owned
prerequisite detector, MessageBox, runtime URL, or native bootstrapper was
involved.

## Open Questions

- Decide whether Authenticode is warranted after the bootstrap distribution
  and Defender false-positive behavior are validated independently.
- Validate backend protocol and error rendering; adapter enrollment,
  tailnet listener reachability, GUI state, logout, node removal; fresh
  installation, Update, Repair, Uninstall, rollback, Start/Stop/Start,
  Watchdog startup configuration, and two-bootstrap self-update in a signed-in
  Windows session.

## Suggested Next Steps

Perform live Windows acceptance for the bootstrap archive, fresh install,
update, rollback, LAN access, and ephemeral Tailscale lifecycle before moving
this design beyond Partially landed.
