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
`startAfterHealthy` list is observed healthy. Dependencies affect bootstrap
only; they do not turn healthy independent processes into a shared failure
domain after startup.

Independent resident control processes, including Evidence and Visual Log, may
be explicit targets in that graph so their authenticated control APIs remain
available. Process recovery never starts a recording or inference run; those
capabilities remain explicitly on demand.

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
startup grace period, and circuit breaker. There is no alternate task lookup,
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
| `visual-log` | `["event-stream", "evidence-recorder"]` |

This ordering is watchdog-owned configuration only. None of the five target
executables reads or references it. Evidence Recorder and Visual Log remain
independent executables even when this graph supervises their availability.

## Local evidence

The status file is atomically replaced before a cycle, after every target, and
after the cycle completes. It records the watchdog PID and start time, cycle
timestamps, target state, observation evidence, failure count, and recovery
count. A stale `lastCycleCompletedAt` plus an absent PID distinguishes a dead
watchdog from a currently idle one. File absence alone is not proof of a
watchdog failure unless installation previously verified the file.

The JSONL log records target transitions and recovery decisions. Neither
status nor logging depends on Event Stream, so an Event Stream failure cannot
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

The Capture Agent/Event Stream and Action OSD installers default to
`WatchdogManaged`: they register on-demand Tasks with no triggers and a zero
restart count, then start them once for installation acceptance. The Evidence
Recorder/Visual Log installer registers independent triggerless Tasks with zero
task-level restart; their Watchdog targets keep the resident services available.
A developer who deliberately needs the old
independent startup behavior for Capture/Event/OSD must pass `-StartupMode
Standalone`; it is never selected automatically.

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
- Startup dependencies are ordered deterministically; unresolved prerequisites
  leave dependents `BLOCKED` without attempting their recovery action.
