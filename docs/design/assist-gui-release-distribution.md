# Assist GUI and executable release distribution

## Current Status

Partially landed.

The repository now has one strict multi-process release catalog, deterministic
SHA-256 output, a tag-driven GitHub Release workflow, a native
`windows-assist-gui.exe`, and an independent `windows-tailscale-adapter.exe`.
The GUI reports local Agent health, private-LAN endpoints, and TailscaleAdapter
state. It can start an ephemeral adapter from a one-off auth key and request a
graceful logout through a local stop file.

The GUI can download the latest public catalog and checksum list over an
HTTP/1.1-only client, stage and verify the base executable set, and hand the
transaction to a verified staged AssistGUI copy running elevated. The
transaction installs or updates current-user Scheduled Tasks, preserves owned
task definitions and installed files for rollback, then requires health,
listener-owner, executable-path, and interactive-session read-back before it
reports success. No GitHub Release has been published or live-installed by
this work.

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
- verified HTTP/1.1 release downloads;
- the optional ephemeral Tailscale transport process; and
- the future whole-release installation and update transaction.

It does not redefine Capture Agent, Event Stream, delegated Pi, Rule, Action,
Watchdog, or inference-runtime semantics. It does not make Tailscale mandatory
and does not silently select it when LAN access fails.

## Release model

`internal/releasecatalog.Specs` is the executable-set authority. Each artifact
has one stable name, role, class, expected Windows PE subsystem, byte count, and
SHA-256 digest. The current classes are:

- `bootstrap` for AssistGUI;
- `runtime-required` for the minimum Agent process graph;
- `runtime-optional` for independently enabled companion processes;
- `operator-tool` for clients and offline tools; and
- `diagnostic` for binaries that must not replace installed GUI artifacts.

The generator fails on missing or unexpected executables, duplicate names,
unsafe paths, metadata drift, invalid PE subsystem, or noncanonical digest.
GitHub Release uploads the individual `.exe` files together with
`windowsagent-release.json` and `SHA256SUMS`.

The catalog is a coherent release set. AssistGUI must stage and validate the
complete selected set before mutating an installation; it must not report a
partially updated process graph as a successful release update.

## AssistGUI model

TailscaleAdapter is disabled by default. AssistGUI always reports Agent health
and active private-LAN IPv4 endpoints. When the adapter is enabled it also
reports `STARTING`, `ONLINE`, `STOPPING`, or `FAILED`, plus assigned Tailscale
IPv4 and IPv6 addresses when available.

The auth key is accepted through a password edit control, passed to the adapter
on standard input, cleared from the control after process start, and never
placed in command-line arguments or status JSON.

The Install / Update action selects the bootstrap artifact, every
`runtime-required` artifact, and TailscaleAdapter. It downloads every selected
file into a fresh staging transaction and writes the verified catalog and
checksum receipt beside them. A staged AssistGUI copy waits for the original
GUI to exit before applying the update, so the installed AssistGUI can replace
itself without overwriting a running executable.

The elevated mutation is restricted to the current user's fixed WindowsAgent
data directory and two WindowsAgent-owned Scheduled Tasks. An existing task
with the same name but a different ownership description is a terminal error.
Rollback restores both task XML and all selected executables and release
metadata. Tailscale remains disabled after installation until the user enables
it and supplies a one-off key.

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

AssistGUI release downloads use a dedicated HTTP/1.1-only `net/http` transport.
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

- Add Authenticode verification in addition to catalog SHA-256 consistency.
- Decide whether optional companion processes need separately selectable
  install controls beyond the current base executable set.
- Validate adapter enrollment, tailnet listener reachability, GUI state, logout,
  node removal, fresh installation, rollback, and self-update in a signed-in
  Windows session.

## Suggested Next Steps

Perform live Windows acceptance for fresh install, update, rollback, LAN access,
and ephemeral Tailscale lifecycle before moving this design beyond Partially
landed.
