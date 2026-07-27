# Observation Script Package

## Current Status

**Partially landed.**

WindowsAgent now verifies and executes the package contract in the internal
runner. The current process binary accepts a host-resolved absolute package
root; replacing that path with an inherited read-only handle remains deferred.
No package is exposed through the unauthenticated HTTP API.

## Decision

An observation task is a digest-pinned package:

```text
<task-id>/
  manifest.json
  TASK.md
  main.star
  output.schema.json
```

The four files are one contract:

- `manifest.json` is the machine-readable identity, permission, and limit
  declaration;
- `TASK.md` explains the task, preconditions, result meaning, privacy impact,
  and known limitations to people and agents;
- `main.star` performs the task through the unified observer API exposed by the
  host;
- `output.schema.json` defines the only JSON result shape that may succeed.

A loose script is not executable. Missing, mismatched, or unpinned package
files fail validation.

## Why Starlark

V1 uses Starlark, embedded through the Go implementation. It is Python-like,
statically checks name binding before execution, and exposes only names
predeclared by the embedding application. WindowsAgent therefore supplies a
small observation API without giving the script ambient shell, process,
network, registry, environment-variable, or filesystem access.

The language choice does not make the script a sufficient security boundary.
The script still runs in `windows-observation-script-runner.exe`, under a
Windows Job Object with deadline, memory, process-count, and output limits.

PowerShell, batch files, JavaScript, Python, native DLLs, arbitrary EXEs, and
dynamic package downloads are unsupported script formats in V1.

Primary references:

- <https://starlark-lang.org/spec.html>
- <https://pkg.go.dev/go.starlark.net/starlark>
- <https://json-schema.org/draft/2020-12>
- <https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects>

## Manifest

Illustrative V1 manifest:

```json
{
  "schemaVersion": 1,
  "id": "crimson-desert/inventory",
  "version": 1,
  "title": "Read the current character inventory",
  "entrypoint": "main.star",
  "taskDocument": "TASK.md",
  "outputSchema": "output.schema.json",
  "files": {
    "main.star": {"sha256": "..."},
    "TASK.md": {"sha256": "..."},
    "output.schema.json": {"sha256": "..."}
  },
  "permissions": {
    "memory": {
      "target": "crimson-desert/current-process",
      "operations": ["modules", "scan", "readBatch"],
      "maxCalls": 8,
      "maxBytesRead": 1048576
    },
    "file": {
      "roots": ["crimson-desert-saves"],
      "operations": ["decode"],
      "maxCalls": 1,
      "maxBytesRead": 67108864
    }
  },
  "limits": {
    "wallTimeMs": 5000,
    "maxSteps": 500000,
    "maxResultBytes": 262144,
    "maxLogBytes": 32768
  }
}
```

All paths are package-relative UTF-8 paths. Absolute paths, traversal,
reparse-point escape, duplicate keys, undeclared files, unknown permission
fields, and a digest mismatch fail package validation.

`permissions` is a maximum authority envelope, not a request to perform every
operation. WindowsAgent intersects it with the locally registered capability.
The script cannot expand either boundary.

The package identity used by a job contains the manifest ID, version, manifest
SHA-256, and a canonical package SHA-256 covering the manifest and every
declared file digest.

## Human Task Document

`TASK.md` must state:

1. what question the task answers;
2. required target state and other preconditions;
3. which memory and file observer APIs it may use and why;
4. the meaning, units, freshness, and stability of every output concept;
5. what the task deliberately does not infer;
6. expected typed failures;
7. privacy and data-retention characteristics;
8. compatibility evidence and known unsupported builds.

The document is descriptive, not executable authority. A statement in
`TASK.md` cannot grant an API permission absent from `manifest.json`.

## Script Entrypoint

`main.star` defines:

```python
def main(ctx):
    # Use only predeclared APIs and JSON-compatible values.
    return {"schemaVersion": 1}
```

`ctx` contains immutable job inputs and non-secret target metadata authorized
by the package. The return value must recursively contain only JSON-compatible
null, boolean, integer, finite number, string, list, and string-keyed
dictionary values.

The result is the return value of `main`; stdout is not an output channel.
`print` is disabled in V1. Privacy-minimized diagnostics use a bounded host
logging API and go to stderr.

`load()` is disabled in V1. A package is one entrypoint, not an implicit module
or dependency resolver. This keeps package identity and review complete.

## Predeclared APIs

The script sees one predeclared `observer` API with two resource namespaces:

```text
observer.memory.modules()
observer.memory.regions(max_regions)
observer.memory.scan(pattern, regions, max_matches)
observer.memory.resolve_rip(address, displacement_offset, instruction_length)
observer.memory.read_batch(reads)
observer.memory.read_strided(base_address, count, stride, fields)

observer.file.stat(path)
observer.file.read(path, offset, length)
observer.file.hash(path, algorithm)
observer.file.decode(path, decoder, options)

job.input(name)
job.attempt(source, function)
job.fail(code, message)
```

An unavailable permission means the corresponding namespace or operation is
absent. Every call is brokered by WindowsAgent and checked against the
manifest, registered capability, target identity, cumulative call count, byte
budget, and deadline before it reaches the job's unified observer process.

Memory operations are read-only and bound to one resolved process identity.
File operations are read-only and bound to named roots; script paths are
root-relative logical paths, never host absolute paths.

`file.decode` selects one host-registered, exact decoder identity. It does not
load code from the script package or dynamically discover a parser. An absent
or mismatched decoder is an explicit failure.

V1 exposes no memory write, file write, directory enumeration, watch,
subscription, timer, sleep, network, process launch, environment, registry, or
UI automation API.

## Output Contract

`output.schema.json` uses JSON Schema Draft 2020-12. It must be self-contained;
remote `$ref` resolution is forbidden. Schemas should close object shapes with
`additionalProperties: false` or `unevaluatedProperties: false`.

Success requires all of the following:

1. module initialization and `main(ctx)` complete within limits;
2. every brokered observer response is valid and authorized;
3. `main` returns a JSON-compatible value;
4. canonical serialization fits `maxResultBytes`;
5. the value validates against the exact pinned output schema.

The runner never repairs, truncates, coerces, or fills missing output fields.
Schema failure is terminal `OUTPUT_SCHEMA_INVALID`.

## Failure Boundary

There is no implicit retry, alternate script version, alternate process,
alternate file selection, cached output, OCR fallback, or partial-success
conversion.

A reviewed package may declare a finite source order with `job.attempt`, such
as process memory followed by one explicitly selected save. The failed attempt
must be visible in the accepted output. Only typed source failures are eligible
to continue. Permission, protocol, host integration, budget, deadline, script,
and output-contract failures remain terminal and cannot be caught by
`job.attempt`.

## Review And Registration

A package becomes runnable only after it is installed in the trusted script
registry and bound to a local capability. Registration verifies all digests,
permissions, limits, schema validity, task-document presence, and static
Starlark validity.

V1 does not accept script bodies or package paths from the unauthenticated HTTP
API. A caller selects a registered package capability and supplies only the
inputs declared by that capability.

## Suggested Next Steps

1. Freeze and publish the manifest JSON Schema.
2. Pass package content through a read-only inherited handle.
3. Add a registry binding package identity to an application capability.
4. Package and register the pinned Crimson save decoder before registering the
   fixture-verified Crimson package as a live capability.
