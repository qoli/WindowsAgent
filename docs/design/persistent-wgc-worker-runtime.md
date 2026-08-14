# Persistent WGC Worker Runtime

## Status

**Landed.** The versioned worker protocol, Agent-side generation
owner, borderless persistent WGC/D3D11 runtime, build/install/update
integration, capture-activity projection, and non-replay tests are
implemented and accepted in the signed-in Windows session. Acceptance verified
`borderlessAccess=allowed`, `borderRequired=false`, no yellow capture border,
a 500 ms capture pulse with capture exclusion, sixty alternating region/OCR
calls on one generation, one deliberately failed in-flight capture without
Agent restart or request replay, and a healthy borderless second generation.

## Responsibility

`windows-wgc-worker.exe` is the crash boundary for the Capture Agent's full
and region captures. One healthy worker generation owns one primary-monitor
WGC capture item/session, D3D11 device and immediate context, two-frame pool,
and region compute shader. It executes all requests serially on the same locked
Windows runtime thread and acquires a frame after accepting each request.

The Capture Agent owns the worker through private framed stdin/stdout. The
worker is not a service, Watchdog target, Action, resident inference runtime,
or public endpoint. Closing the pipe ends an idle worker. A dedicated Windows
parent-process waiter terminates the worker if the Agent exits even while the
WGC thread is blocked inside native code, so an Agent crash cannot leave an
orphaned capture session. The independent Evidence recorder and finite
Observer jobs retain their own capture lifecycles.

The Agent starts and negotiates the initial worker generation during Agent
startup with a bounded two-minute initialization deadline. The worker requests
Windows borderless-capture access, sets `IsBorderRequired=false`, reads the
property back, and only then starts capture. The initialize response reports
`borderlessAccess=allowed` and `borderRequired=false`; the Agent verifies both.
A denied permission, unsupported interface, deadline, or property mismatch
fails initialization explicitly. There is no bordered-capture fallback.

## Failure contract

There is no capture-provider or in-process fallback. A protocol EOF or region
`capture_readback_failed` retires that generation. Because a region capture is
an idempotent observation, one unchanged request may run once on a fresh
generation after a readback failure. Transport EOF retains its separate
five-attempt bound. Each attempt acquires a new frame and revalidates
capture-time foreground identity; no pixels or metadata from a failed attempt
are returned. Recovery, exhaustion, generation, and process identifiers remain
visible in runtime logs. Caller cancellation and the original absolute
deadline bound the complete retry set. Full-capture provider failures,
non-transient failures, and an exhausted region recovery retain the exact final
cause and remain terminal. There is no capture backend, provider, algorithm,
or cached-frame fallback.

Worker stderr is bounded and forwarded into the Agent's runtime diagnostics.
The installer and complete binary deployment path configure process-scoped
Windows Error Reporting full dumps for both the Agent and worker. Dumps and
runtime logs remain private operator data.

## Protocol and lifecycle

The first message must negotiate the exact protocol version and return the
worker PID, WGC backend, persistence flag, verified border state, and current
monitor support. Initialization and every later call carry distinct bounded
absolute deadlines. Unknown JSON fields, trailing values, mismatched response
identifiers, oversized frames, and unsupported methods fail explicitly.

After accepting a full or region capture request, the Agent publishes a
session-local recent-capture pulse. The pulse contains no frame, request data,
foreground identity, or success claim and stays active for at least 500 ms
after the latest accepted request. Notification failure is logged once and
does not change capture execution. The OSD is an optional observer; it cannot
invoke, cancel, retry, or make a capture succeed.

The adapter exposes the existing generic `capture.Capturer` and
`capture.RegionCapturer` interfaces. Worker construction, restart policy, RPC,
and process diagnostics remain hidden inside `internal/wgcworker`; WGC and
D3D11 pointer ownership remain hidden inside `internal/wgc`.

## Acceptance boundary

Local Go tests and cross-compilation prove only the protocol and build
contract. Live acceptance must run in the signed-in interactive Windows
session and record:

1. the installed Agent and worker hashes, interactive session, protocol
   version, worker generation, and PID;
2. repeated alternating OCR-sized and heat-sized region captures through the
   real Action path, plus full capture, without Agent restart;
3. one deliberately failed transient in-flight capture where the current
   generation is retired and the unchanged idempotent request succeeds on
   exactly one fresh generation, plus an exhausted case that remains terminal;
4. current foreground identity and domain postconditions independently from
   transport and process health;
5. absence of fallback, changed-request replay, stale-frame substitution,
   orphaned workers, and unbounded crash artifacts;
6. Windows reports borderless access allowed and `IsBorderRequired=false`, no
   yellow capture border remains, and the capture pulse appears without being
   included in captured output or stealing focus.
