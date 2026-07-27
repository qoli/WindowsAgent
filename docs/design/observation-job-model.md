# Scripted Observation Job Model

## Current Status

**Partially landed.**

The repository now implements package verification, the bounded Starlark
runner, the unified observer call surface, strict framed transport, and a
Windows native suspended/Job Object launcher. The two child binaries build.

The capture agent does not yet own the application-level broker that launches
both children and forwards `broker/call`, so scripted observation is not yet a
registered public WindowsAgent capability.

`ObservationJob` is the application execution record. It is distinct from the
Windows `ProcessJobObject` used to contain the script runner and observer
processes.

## Core Invariant

```text
one ScriptObservationJobSpec
  -> one verified Script Package
  -> one isolated script-runner process
  -> one isolated unified observer process
  -> zero or more brokered observer API calls
  -> one schema-valid JSON value or one typed terminal failure
```

The script controls task logic. WindowsAgent retains authority over target
resolution, permissions, process launch, protocol validation, resource limits,
and final output acceptance.

A job is synchronous and bounded. V1 has no queue, scheduler, retry, resume,
background watch, or recurring execution.

## Identity

| Identity | Owner | Meaning |
| --- | --- | --- |
| `jobId` | WindowsAgent | One application-level execution |
| `scriptRunId` | launcher | One script-runner process instance |
| `observerCallId` | broker | One observer API call |
| `observerId` | launcher | The job's unified observer process instance |
| JSON-RPC `id` | transport | One framed request/response correlation |

There is no `observationId`. IDs from different layers are never substituted
for one another.

## Job Specification

```go
type ScriptObservationJobSpec struct {
    Version       uint32
    JobID         string
    Deadline      time.Time
    Capability    CapabilityIdentity
    ScriptPackage ScriptPackageIdentity
    Inputs        map[string]json.RawMessage
}

type ScriptPackageIdentity struct {
    ID             string
    Version        uint32
    ManifestSHA256 string
    PackageSHA256  string
}
```

The registered capability binds the exact allowed package identity, input
schema, target selectors, permission ceiling, and local policy. A generic
caller cannot supply an arbitrary package path, PID, absolute file path, or
permission.

`Deadline` is an absolute UTC deadline and may only reduce the package wall
time. `Inputs` must validate against the capability's input schema. No field
has a production default.

## Construction

Example:

```text
request capability: crimson-desert/inventory.read
  -> resolve registered package crimson-desert/inventory@1
  -> resolve exact current CrimsonDesert.exe process identity
  -> intersect package permissions with local capability policy
  -> validate caller inputs
  -> freeze ScriptObservationJobSpec
```

The request selects semantic intent. It does not contain Starlark, signatures,
pointer chains, memory addresses, filesystem roots, or shell commands.

## Execution API

```go
type ObservationJobRunner interface {
    Run(
        ctx context.Context,
        spec ScriptObservationJobSpec,
    ) (ScriptObservationJobResult, error)
}
```

V1 permits one active scripted observation job. A concurrent call fails with
`JOB_BUSY`; it is not queued.

## Lifecycle

```text
created
  -> validating-package
  -> resolving-authority
  -> launching-runner
  -> executing-script
       -> brokering-observer-call
       -> executing-script
  -> validating-output
  -> stopping
  -> succeeded
```

Every non-terminal state may transition to `failed` or `cancelled`.

The broker sub-state may occur zero or more times. That is not polling: every
call must originate from the finite script execution and consume declared
budgets. The package may combine memory and file evidence in one job when both
permissions are declared.

## Process Ownership

WindowsAgent launches `windows-observation-script-runner.exe` and
`windows-observer.exe` directly with `CreateProcessW`; it never uses
PowerShell, `cmd.exe`, a batch file, or `ShellExecute`.

Before resuming the suspended runner, WindowsAgent assigns it to a Windows Job
Object configured with at least:

- `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`;
- active-process limit;
- process memory limit;
- job memory limit;
- job CPU or time limit where supported.

The runner cannot launch or connect to the observer. WindowsAgent starts one
unified observer for the job, after authority and limits are frozen. When a
Starlark built-in is called, the runner sends a broker request to WindowsAgent
over its framed stdio channel. WindowsAgent authorizes it, forwards it to the
job's observer, validates the response, and returns the bounded value to the
runner.

The observer may handle multiple finite calls during one script execution, but
it has no autonomous sampling loop, timer, watch, subscription, polling, or
background task. It exits when the job terminates. This keeps all process
creation and authority in the Go host while avoiding duplicated memory/file
process protocols.

## Limits

Effective limits are the minimum of package, capability, host policy, and
remaining caller deadline:

```go
type EffectiveJobLimits struct {
    WallTime       time.Duration
    ScriptSteps    uint64
    ObserverCalls  uint32
    MemoryReadBytes uint64
    FileReadBytes  uint64
    ResultBytes    uint64
    LogBytes       uint64
}
```

Exceeding any limit is terminal. The host does not raise a limit to help a
script finish.

## Result

```go
type ScriptObservationJobResult struct {
    JobID         string
    Capability    CapabilityIdentity
    ScriptPackage ScriptPackageIdentity
    OutputSchema  SchemaIdentity
    StartedAt     time.Time
    FinishedAt    time.Time
    Output        json.RawMessage
    Provenance    []ObserverCallProvenance
}
```

`Output` is the canonical JSON serialization of the script return value after
successful validation against the package's pinned `output.schema.json`.

Provenance records operation names, observer binary identity, resolved target
identity, timestamps, byte counts, and response digests. It excludes raw
memory bytes and file content unless the output contract explicitly includes
them.

## Failure

```go
type ScriptObservationJobError struct {
    JobID   string
    Stage   string
    Code    string
    Message string
}
```

Required codes include:

- `SCRIPT_PACKAGE_INVALID`
- `SCRIPT_PERMISSION_DENIED`
- `SCRIPT_STATIC_INVALID`
- `SCRIPT_RUNTIME_FAILED`
- `SCRIPT_LIMIT_EXCEEDED`
- `OBSERVER_CALL_FAILED`
- `OBSERVER_PROTOCOL_INVALID`
- `OUTPUT_NOT_JSON`
- `OUTPUT_SCHEMA_INVALID`
- `JOB_DEADLINE_EXCEEDED`
- `JOB_CANCELLED`

Failures do not produce stale, cached, or partially repaired results. A package
may deliberately return a result from the next declared source after a typed
source failure, but the accepted schema must expose that source and the failed
attempt. Infrastructure, permission, protocol, budget, deadline, script, and
schema failures remain terminal.

## Persistence

V1 persists only accepted result JSON plus bounded provenance, or a
privacy-minimized terminal failure record. It does not persist raw memory
buffers, full source files, Starlark interpreter state, or resumable jobs.

## Relationship To Other Docs

- [Observation Script Package](observation-script-package.md) owns the package,
  language, manifest, task document, and output schema.
- [Windows Observation Process Protocol](observation-worker-protocol.md) owns
  runner/broker and observer wire contracts.
- [Crimson Desert Inventory Script](../examples/crimson-desert-inventory-job.md)
  is the concrete inventory example.

## Explicitly Unsupported In V1

- arbitrary or caller-supplied scripts;
- long-running daemons, watch loops, subscriptions, and timers;
- observer process reuse across jobs;
- script-launched child processes;
- hidden retries or fallback data sources;
- dynamic dependencies or remote schema resolution.

## Suggested Next Steps

1. Add the host-side broker that connects one runner and one observer using
   `internal/observationlauncher`.
2. Replace the runner's host-resolved package path with a read-only inherited
   package handle.
3. Register a fixture-only capability before exposing any live capability.
4. Register Crimson Desert inventory only after its pure-read runtime root is
   validated on the supported build.
