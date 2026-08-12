# Persistent WGC Worker Runtime

## Status

**Partially landed.** The versioned worker protocol, Agent-side generation
owner, persistent WGC/D3D11 runtime, build/install/update integration, and
non-replay tests are implemented. Signed-in Windows acceptance under the
previously crashing alternating region-capture workload remains required
before this design can be marked Landed.

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

## Failure contract

There is no capture-provider or in-process fallback. A protocol failure,
deadline, worker exit, native access violation, or WGC/D3D11 capture error
fails the current call and retires that generation. The Agent never replays the
failed request because its input or foreground postcondition may no longer be
current. A later independent request may create a new generation explicitly
visible through generation and process identifiers in lifecycle logs.

Worker stderr is bounded and forwarded into the Agent's runtime diagnostics.
The installer and complete binary deployment path configure process-scoped
Windows Error Reporting full dumps for both the Agent and worker. Dumps and
runtime logs remain private operator data.

## Protocol and lifecycle

The first message must negotiate the exact protocol version and return the
worker PID, WGC backend, persistence flag, and current monitor support. Every
later call carries a bounded absolute deadline. Unknown JSON fields, trailing
values, mismatched response identifiers, oversized frames, and unsupported
methods fail explicitly.

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
3. one deliberately terminated worker where the in-flight request fails once,
   the Agent remains healthy, and the next independent request starts exactly
   one new generation;
4. current foreground identity and domain postconditions independently from
   transport and process health;
5. absence of fallback, request replay, stale-frame substitution, orphaned
   workers, and unbounded crash artifacts.
