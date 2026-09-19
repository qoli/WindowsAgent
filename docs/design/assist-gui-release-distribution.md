# Assist GUI and executable release distribution

## Current Status

Partially landed.

The repository now has one strict multi-process release catalog, deterministic
SHA-256 output, a tag-driven GitHub Release workflow, a self-contained C#
WinUI 3 `windows-assist-gui.exe`, its Go `windows-assist-backend.exe`, and an
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
bootstrap transport. That archive contains exactly `windows-assist-gui.exe`
and `windows-assist-backend.exe`; it does not duplicate the release catalog,
checksums, or any runtime executable, and it does not hide the multi-process
release inside a combined archive.

The catalog is a coherent release set. AssistGUI must stage and validate the
complete selected set before mutating an installation; it must not report a
partially updated process graph as a successful release update.

## AssistGUI setup surface

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

The GUI exposes the concrete operations Install, Update, Repair, Uninstall,
Start WindowsAgent, and Stop WindowsAgent. It reports Installed/Not installed,
Capture Agent Running/Stopped, installed version, Watchdog Running/Stopped and
start-at-sign-in state, LAN endpoints, and Tailscale status and assigned IPs.
A staged backend copy waits for both the original GUI and original backend to
exit before applying a mutation, so the bootstrap pair can replace its
installed copies without overwriting running executables.

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

## Open Questions

- Decide whether Authenticode is warranted after the bootstrap distribution
  and Defender false-positive behavior are validated independently.
- Validate WinUI single-file extraction/startup without a preinstalled Windows
  App Runtime; backend protocol and error rendering; adapter enrollment,
  tailnet listener reachability, GUI state, logout, node removal; fresh
  installation, Update, Repair, Uninstall, rollback, Start/Stop/Start,
  Watchdog startup configuration, and two-bootstrap self-update in a signed-in
  Windows session.

## Suggested Next Steps

Perform live Windows acceptance for the bootstrap archive, fresh install,
update, rollback, LAN access, and ephemeral Tailscale lifecycle before moving
this design beyond Partially landed.
