# Windows Watchdog

## Status

**Landed, not installed by default.**

`windows-watchdog.exe` is an optional external observer and bounded recovery
process for independently installed WindowsAgent processes. It has one-way
coupling: the watchdog knows how to observe its configured targets, while no
target imports, registers with, emits a heartbeat to, or otherwise knows about
the watchdog.

The watchdog is deliberately not highly available. Its Scheduled Task starts
at interactive-user logon and has a zero restart count. If the watchdog exits,
it remains stopped. Monitored processes continue independently and are not
children of a watchdog-owned Job Object. The last atomically published status
and append-only local log remain as evidence for an operator.

## Boundary

The watchdog accepts one strict external JSON configuration. Adding or
removing a target changes only this watchdog-owned file. Target executables,
Rule packages, runtime manifests, HTTP contracts, and build outputs remain
unchanged.

The watchdog is the AtLogOn entrypoint for watchdog-managed modules. Their
Scheduled Tasks are registered without triggers and with a zero restart count. On its
first cycle, the watchdog reconciles every explicit `desiredState: "running"`
target in dependency order. An absent or unhealthy bootstrap target is acted on
immediately rather than waiting for the steady-state failure threshold. A
dependent target remains `BLOCKED` until every target in its
`startAfterHealthy` list is observed healthy. The same dependency gate applies
before every later recovery action. A blocked recovery does not consume an
attempt. After bootstrap, healthy dependents continue to be observed normally
when an upstream target is unhealthy; only a pending restart is blocked. This
does not turn healthy independent processes into a shared failure domain.

Independent resident control processes, including Evidence and Visual Log, may
be explicit targets in that graph so their authenticated control APIs remain
available. Process recovery never starts a recording or inference run; those
capabilities remain explicitly on demand.

The Event Web projection is also a watchdog-managed resident process. It starts
only after Event Stream is healthy, preventing an interactive-logon race where
its required event source is unavailable and startup exits terminally. A
private-LAN-only Event Web listener uses an exact process probe because watchdog
HTTP probes are deliberately restricted to loopback; live acceptance separately
verifies the configured `/healthz` endpoint.

The watchdog understands only two generic probe types:

- `process`: require exactly one process at an exact absolute executable path,
  with an explicit decision about whether Session 0 is forbidden;
- `http-json`: require an exact loopback URL, status code, and strict
  `{"status":"..."}` response.

Every configured probe must pass. A probe implementation error such as denied
process enumeration becomes `OBSERVATION_FAILED` and cannot trigger recovery,
because the target's state was not established. A valid unhealthy observation
increments the target's consecutive failure count.

After the configured threshold, the watchdog may restart only the exact
Scheduled Task named by the target. Before mutation it verifies the task's
description, preventing a configuration typo from operating an unrelated
task. Restart attempts have an explicit window, backoff, action timeout,
startup grace period, and circuit breaker. The startup grace period begins
after the Scheduled Task restart returns; time spent observing or stopping
the previous process does not consume it. Probes continue during this window:
the first healthy observation ends startup protection immediately, while an
unhealthy observation remains `STARTUP_GRACE` without counting as a runtime
failure or causing another restart. The status exposes `startupDeadline` and
the latest probe evidence. When that deadline expires, the configured failure
threshold and recovery budget apply again. An observation error remains
`OBSERVATION_FAILED`, never a successful readiness result.

`startupGraceMs` is a per-target initialization budget, not a fixed delay or a
steady-state HTTP timeout. Size it against the target's actual startup
contract. Assist setup gives Capture Agent 125 seconds to cover its two-minute
WGC initialization budget and five-second required Event Stream health check;
early readiness avoids delaying dependents for that full duration. Existing
operator-owned configurations remain explicit and are not silently rewritten.

There is no alternate task lookup,
PID-only kill, direct executable launch, guessed path, port substitution, or
provider fallback.

The watchdog never interprets Action events, retries an Action invocation,
replays input, starts an action-scoped visual runtime, or repairs configuration.
Processes owned inside Capture Agent or an Action remain the responsibility of
their existing owner; the watchdog targets only explicitly installed
long-lived Scheduled Tasks.

## Configuration

All durations are integer milliseconds and all fields are required unless a
probe type makes them inapplicable:

```json
{
  "schemaVersion": 1,
  "checkIntervalMs": 5000,
  "targets": [
    {
      "id": "event-stream",
      "desiredState": "running",
      "startAfterHealthy": [],
      "failureThreshold": 3,
      "probes": [
        {
          "type": "process",
          "executablePath": "C:\\Users\\operator\\AppData\\Local\\gameGuide\\windows-capture-agent\\bin\\windows-event-stream.exe",
          "requireInteractiveSession": true
        },
        {
          "type": "http-json",
          "url": "http://127.0.0.1:8788/healthz",
          "timeoutMs": 2000,
          "expectedStatusCode": 200,
          "expectedJsonStatus": "ok"
        }
      ],
      "recovery": {
        "scheduledTaskName": "gameGuide Windows Event Stream",
        "expectedTaskDescription": "gameGuide durable local event stream; interactive-user session required",
        "maxAttempts": 3,
        "attemptWindowMs": 300000,
        "backoffMs": 30000,
        "actionTimeoutMs": 20000,
        "startupGraceMs": 10000
      }
    }
  ]
}
```

Configuration is never generated from running processes. The operator must
provide exact installed paths and Scheduled Task ownership evidence. Use
`--validate-only` to validate without probing or mutating targets, and `--once`
to execute one real cycle.

Dependency IDs must exist, be unique per target, and form an acyclic graph.
Unknown dependencies, self-dependencies, cycles, and any desired state other
than the currently implemented explicit `running` state reject the complete
configuration before observation or mutation begins.

A typical installed graph is:

| Target | `startAfterHealthy` |
|---|---|
| `event-stream` | `[]` |
| `evidence-recorder` | `[]` |
| `capture-agent` | `["event-stream"]` |
| `action-osd` | `["event-stream"]` |
| `event-web` | `["event-stream"]` |
| `visual-log` | `["event-stream", "evidence-recorder"]` |
| `sftp` | `[]` |

This ordering is watchdog-owned configuration only. None of the seven target
executables reads or references it. Evidence Recorder and Visual Log remain
independent executables even when this graph supervises their availability.

## Local evidence

The status file is atomically replaced before a cycle, after every target, and
after the cycle completes. It records the watchdog PID and start time, cycle
timestamps, target state, observation evidence, failure count, and recovery
count. A stale `lastCycleCompletedAt` plus an absent PID distinguishes a dead
watchdog from a currently idle one. File absence alone is not proof of a
watchdog failure unless installation previously verified the file.

On Windows, a reader can briefly deny replacement sharing while it has the
current status file open. Publication retries only the same atomic rename for a
bounded one-second window when Windows reports access denied or a sharing
violation. It never switches to an in-place or non-atomic write; a non-transient
error or exhausted retry budget remains terminal and preserves the cause.

The JSONL log records target transitions and recovery decisions. Unhealthy
observations preserve the exact probe evidence, including process PID/session,
per-probe duration, and the configured HTTP timeout and transport error when
the request fails. Status also exposes the latest total observation and
recovery durations. Recovery logs identify the attempt, duration, and startup
deadline so slow Task Scheduler calls can be distinguished from slow target
initialization. Event Stream logs its PID and `journal-open` phase before
scanning the journal, and records startup duration or the original scan error.
Neither status nor logging depends on Event Stream, so an Event Stream failure cannot
blind the watchdog.

## Installation

Build the GUI-subsystem executable, author and validate an environment-specific
configuration, then install it in the signed-in interactive session:

```powershell
.\scripts\install-windows-watchdog.ps1 `
  -ExecutablePath .\.build\windows-watchdog.exe `
  -ConfigPath .\watchdog-config.json
```

The installer verifies the source and installed configuration, executable
hash, GUI PE subsystem, interactive session, initial status publication, and
the registered task's zero restart count. It does not modify a firewall,
install a service, modify any monitored module, or grant the watchdog
self-recovery.

The Capture Agent/Event Stream, Action OSD, Event Web, and SFTP installers
default to `WatchdogManaged`: they register on-demand Tasks with no triggers
and a zero restart count, then start them once for installation acceptance. The
Evidence Recorder/Visual Log installer registers independent triggerless Tasks
with zero task-level restart; their Watchdog targets keep the resident services
available. A developer who deliberately needs independent startup behavior for
Capture/Event/OSD/Web/SFTP must pass `-StartupMode Standalone`; it is never
selected automatically.

## Acceptance

- Removing or stopping the watchdog leaves every monitored process running.
- Killing the watchdog leaves its Scheduled Task stopped with non-zero result;
  it is not automatically restarted.
- Killing one configured target results in a bounded restart of only its exact
  owned Scheduled Task.
- Changing the expected task description makes recovery fail without mutation.
- Exhausting the attempt budget enters `CIRCUIT_OPEN` without further actions.
- An unreadable process snapshot enters `OBSERVATION_FAILED` without recovery.
- A malformed or missing configuration prevents watchdog startup explicitly.
- Startup and recovery dependencies are ordered deterministically; unresolved
  prerequisites leave pending restarts `BLOCKED` without consuming attempts.
- A healthy dependent stays healthy and is still observed when its prerequisite
  fails; dependency changes alone do not restart it.
- Readiness can finish the startup window early; repeated unhealthy observations
  within the configured startup budget do not restart the initializing process.
- A slow restart does not consume the startup budget, and an expired startup
  budget still respects the configured failure threshold and recovery limits.

### Opt-in interactive Windows acceptance

Build the native fixture runner from the same checkout:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c \
  -o .build/watchdog-acceptance.test.exe ./internal/watchdog
```

Run it in the signed-in user's interactive Windows session:

```powershell
$env:WINDOWSAGENT_WATCHDOG_LIVE_ACCEPTANCE = '1'
.\watchdog-acceptance.test.exe -test.run='^TestWatchdogLiveAcceptance$' -test.v -test.timeout=90s
```

The runner creates two uniquely named, triggerless, limited-user Scheduled Tasks
with synthetic loopback HTTP targets. It exercises the production process
inspector, Task Scheduler recoverer, status sink, slow initialization, and
dependency recovery under eight seconds of below-normal-priority CPU pressure.
It verifies the observed process-start counts, final readiness and interactive
session, then removes its own Tasks and temporary files. Each fixture Task also
has a two-minute execution limit. Installed target paths and configuration are
outside the fixture's scope. Keep its runtime output outside the public checkout.
