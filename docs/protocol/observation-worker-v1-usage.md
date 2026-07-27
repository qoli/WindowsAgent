# Windows Observer Protocol V1 Usage

## Status

**Partially landed usage guide.**

The child runtimes and native launcher are implemented. Steps 1-4 and the
application broker in steps 8-10 remain host integration work; there is no
public capability endpoint yet.

This guide describes one scripted observation job using:

```text
windows-observation-script-runner.exe
windows-observer.exe
```

The observer is unified. Memory and file access are namespaces in one protocol,
not separate binaries or independently versioned APIs.

## Host Sequence

```text
1. Resolve one registered script capability.
2. Verify manifest, TASK.md, script, schema, and every digest.
3. Validate job inputs and resolve exact process/file authorities.
4. Compute effective permissions and limits.
5. Create private pipe sets and one Windows Job Object.
6. Launch the script runner and unified observer suspended and without windows.
7. Assign both processes to the Job Object, then resume them.
8. Initialize both exact protocol sessions.
9. Send script/run once.
10. Broker each finite observer API call.
11. Receive the script's JSON-compatible return value.
12. Canonically serialize and validate it against output.schema.json.
13. Shut down both children and return one terminal result.
```

No PowerShell, `cmd.exe`, batch file, shell command, service, TCP listener,
sampling loop, file watch, or polling is involved.

## Example Job

```go
spec := ScriptObservationJobSpec{
    Version:  1,
    JobID:    jobID,
    Deadline: deadlineUTC,
    Capability: CapabilityIdentity{
        ID:      "crimson-desert/inventory.read",
        Version: 1,
        SHA256:  capabilitySHA256,
    },
    ScriptPackage: ScriptPackageIdentity{
        ID:             "crimson-desert/inventory",
        Version:        1,
        ManifestSHA256: manifestSHA256,
        PackageSHA256:  packageSHA256,
    },
    Inputs: map[string]json.RawMessage{
        "save": selectedAuthorizedSavePath,
    },
}

result, err := runner.Run(ctx, spec)
```

The caller selects a registered semantic capability. It does not supply a PID,
address, AOB pattern, script body, or executable path. When the capability
declares a save input, WindowsAgent resolves the caller's selection to one
authorized logical-root-relative path; the script cannot enumerate or choose
the “latest” file.

## Runner Call

WindowsAgent sends one `script/run`:

```json
{
  "jsonrpc": "2.0",
  "id": "run-1",
  "method": "script/run",
  "params": {
    "jobId": "7be1e222-2d70-45cb-bfeb-8519544d913d",
    "package": {
      "id": "crimson-desert/inventory",
      "version": 1,
      "packageSha256": "..."
    },
    "inputs": {}
  }
}
```

The intended final contract passes package content through a host-controlled
read-only handle. The current runner accepts a host-resolved absolute
`--package-root`, revalidates every declared file and digest, and exposes no
package path through HTTP. The inherited-handle replacement remains required
before the broker is considered landed.

## Brokered Memory Call

For:

```python
observer.memory.read_batch(reads)
```

the runner emits `broker/call`. WindowsAgent verifies permission and budget,
then forwards a corresponding `observer/call` to the unified observer.

The observer response is validated by WindowsAgent before its `value` is
returned to Starlark.

## Brokered File Call

For:

```python
observer.file.read(
    path = {"root": "crimson-desert-saves", "relative": "slot0/save.dat"},
    offset = 0,
    length = 4096,
)
```

the same `broker/call` and `observer/call` envelopes are used with
`namespace: "file"`. There is no second protocol lifecycle.

Registered structured formats may instead use:

```python
observer.file.decode(
    path = job.input(name = "save"),
    decoder = "crimson-rs/inventory@bb730180",
    options = {"scope": "active-character-inventory"},
)
```

The decoder identity is exact. This is not a general plugin loader.

## Finite Calls, Not Observation Loops

The observer remains alive only for the duration of the one job so that a
script can combine related calls without launching another binary each time.
It does nothing between requests.

Manifest limits bound:

- total observer calls;
- memory bytes read;
- file bytes read;
- script instructions;
- wall time;
- result and diagnostic sizes.

The absence of a sampling/watch loop is an invariant, not merely a suggested
usage pattern.

## Output Acceptance

The script's return value succeeds only when it:

- is JSON-compatible;
- fits the result byte limit;
- validates against the exact package schema;
- is accompanied by valid observer-call provenance;
- completes before the deadline.

Output is returned data, not stdout text. The host never repairs, truncates, or
fills missing fields.

## Failure Behaviour

A reviewed package may use `job.attempt` for a finite source order. For the
Crimson inventory package, that order is memory, then one explicitly selected
save, then terminal failure. Only typed source failures can continue.

Package, permission, protocol, host integration, runtime, limit, deadline, and
schema failures terminate the job immediately. V1 does not retry, enumerate or
select another target/file, use cached data, invoke OCR, or run an alternate
script.

## Conformance Checklist

- exactly one runner and one unified observer are launched per job;
- both are launched directly by WindowsAgent without a visible console;
- the observer exposes one versioned protocol with closed memory/file
  namespaces;
- every call is authorized and counted by WindowsAgent;
- neither child can launch another process;
- scripts have no ambient OS access;
- the final result is one schema-valid JSON value;
- failure remains explicit and terminal.
