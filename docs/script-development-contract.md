# Script Package Development Contract

## Status and authority

This document is the authoring and review contract for every observation
package below `Rules/<Executable.exe>/Actions/` declared by a Rule v5 Action
using `windows-observation-v1`.

An Observation Script Package is trusted local task code, not an untrusted
plugin sandbox. Trust does not remove its boundaries: every input, Observer
operation, native artifact, resource limit, output field, source transition,
and failure must still be explicit and reviewable.

The implemented package loader, Script Runner, Observer, and Job Host are the
source of truth if this document drifts. A package must not depend on behavior
that exists only in documentation or in a private operator environment.

## Package ownership

Each leaf directory owns one finite observation task:

```text
Rules/
`-- <Executable.exe>/
    `-- Actions/
        `-- <task>/
            |-- manifest.json
            |-- TASK.md
            |-- main.star
            |-- input.schema.json
            |-- output.schema.json
            `-- native/                 # optional
                `-- windows-amd64/
                    `-- <artifact>.dll
```

The package owns:

- the task semantics; the stable capability ID, path, and runtime are declared
  by the owning Rule plugin's `rule.json`;
- required host inputs and their meaning;
- source ordering and any explicitly approved fallback;
- game/application-specific process validation, signatures, offsets, data
  conversion, fixed screen profiles, UI coordinates, and pixel interpretation;
- native DLL artifacts, export names, ABI declarations, return codes, handle
  lifecycle, and decoded record layouts;
- the terminal JSON contract and application-level error codes;
- bounds that make every read and allocation finite.

WindowsAgent Core owns only generic execution: package validation, bounded
Starlark, permission enforcement, read-only Observer calls, job-scoped blobs,
generic Windows amd64 FFI, owning-Rule process binding, exact Host resource
bindings, process isolation, deadlines, accounting, and provenance.

Do not move package knowledge into `internal/observer`,
`internal/observationjob`, `internal/scriptrunner`, or a command merely to make
one package easier to implement. The generic launcher resolves the capability
from `rule.json`; it must not allowlist a capability or own that package's
inputs, decoder ABI, or observation logic.

## Required package members

### `TASK.md`

`TASK.md` is the human contract. It must be sufficient for a reviewer to
understand the task without reverse-engineering `main.star`. Include:

1. purpose and exact success condition;
2. required host inputs and who selects them;
3. process, executable build, file-root, and platform preconditions;
4. finite source order, if more than one source is intentionally supported;
5. validation performed before observed bytes become domain data;
6. native artifact identity and owned ABI behavior, when applicable;
7. terminal output meaning and application-level failure codes;
8. privacy restrictions and prohibited output;
9. runtime validation expectations.

Do not describe polling, watching, automatic latest-file selection, inferred
paths, retries, or alternate decoders unless that behavior is an explicitly
approved part of the task contract and is implemented visibly.

### `input.schema.json`

The input schema is the machine-readable launch contract. It must use JSON
Schema Draft 2020-12, close objects with `additionalProperties: false`, require
every semantic input, and bound strings, arrays, and numeric values. Inputs
must use logical resource aliases rather than package paths, executable names,
or private absolute Host paths.

The generic launcher validates inputs before creating child processes. The
Script Runner validates the same input again before executing `main.star`.

### `output.schema.json`

The output schema is the machine contract. It must:

- use JSON Schema Draft 2020-12;
- describe the complete successful result;
- require every semantically required field;
- use `additionalProperties: false` for closed contract objects;
- place finite bounds on arrays, strings, and numeric domains where the task
  has a reviewed bound;
- encode source-specific invariants and attempt order when those affect the
  meaning of success;
- distinguish unavailable values with an explicit nullable field instead of
  omitting fields unpredictably;
- exclude raw memory, save bytes, private paths, credentials, and unbounded
  diagnostics.

`main(ctx)` returning JSON is not success by itself. The Runner rejects a
result that is not JSON-compatible, exceeds `maxResultBytes`, or fails the
pinned schema.

### `manifest.json`

The manifest is the executable package contract. V2 requires:

- `schemaVersion: 2`;
- a positive integer `version`;
- distinct `entrypoint`, `taskDocument`, `inputSchema`, and `outputSchema`
  members;
- `taskDocument: "TASK.md"`;
- every regular package file declared exactly once in the `files` path list;
- least-privilege Observer permissions;
- bounded script limits;
- each optional native DLL declared in both `files` and `nativeLibraries`.

Package paths use forward slashes, are relative to the package root, and may
not contain traversal, drive letters, backslashes, or symlink escapes.
Undeclared files, missing files, unknown manifest fields,
duplicate strict-JSON keys, and package members larger than 4 MiB fail loading.
`manifest.json` itself is not listed in `files`.

The current allowed permission operations are:

```text
memory: modules, regions, scan, resolveRip, readBatch, readStrided
file:   list, stat, read, hash, openBlob
screen: readRegion
```

Manifest operations use the camel-case names above. Starlark receives only
declared operations, exposed in snake case, for example `resolveRip` becomes
`observer.memory.resolve_rip`.

Permission targets and file-root names are logical bindings. File-root
declarations use a supported portable resolver such as
`windows-known-folder/LocalAppData` plus a canonical relative path. They must
not encode a private machine identity or absolute path.

Screen permissions require `maxCalls: 1` and positive `maxPixels` no greater
than 65,536. Every `readRegion` call must provide `x`, `y`, `w`, and `h` inside
the fixed 1920x1080 reference coordinate space and an explicit `sampling`
value of `reference` or `native`. Core maps through a centered 16:9 viewport.
Reference sampling returns exactly `w` by `h` pixels; native sampling preserves
the mapped physical density. `maxPixels` bounds the returned image in either
mode. Invalid mapping, unknown sampling, foreground identity drift, malformed
pixel evidence, shader failure, and budget exhaustion are terminal. UI
location, sampling choice, color rules, and evidence thresholds belong to the
package, not Core.

The implemented memory target is exactly `rule/current-process`. The launcher
derives its executable from the owning `Rules/<Executable.exe>/` folder.
File-root aliases must be canonical and unique. The Host resolves every
declaration locally. Launch requests cannot bind, override, or add roots.

Set limits from reviewed worst-case behavior, not from the largest convenient
number:

- `maxCalls` must cover the exact finite call graph;
- `maxBytesRead` must cover the maximum authorized bytes;
- `wallTimeMs` and `maxSteps` must terminate runaway work;
- `maxResultBytes` must fit the schema's maximum useful result;
- `maxLogBytes` must be a positive package log budget. V2 discards Starlark
  `print`; the Host separately truncates child-process stderr, and neither
  channel authorizes sensitive logging.

At least one memory permission, file permission, screen permission, or native
library is required.
V2 permits at most four native libraries. Each library must have a canonical
alias, `windows-amd64`-style platform, package-relative `.dll` artifact,
positive call limit no greater than 1024, and positive native memory limit no
greater than 1 GiB.

### `main.star`

The entrypoint must define:

```python
def main(ctx):
    return {
        "schemaVersion": 1,
    }
```

Its returned value must contain only JSON-compatible Starlark values. Starlark
`load` statements are forbidden. The runtime does not provide network access,
process launch, environment access, unbounded directory enumeration, timers,
sleep, polling, or file watching. A package receives `observer.file.list` only
when explicitly declared, and every call must include bounded depth and entry
limits. `print` output is discarded so stdout remains a framed protocol
channel.

The deterministic standard Starlark `math` module is predeclared. Packages may
use its finite numeric functions such as `math.hypot`, `math.atan2`,
`math.degrees`, and `math.round`; non-finite results remain invalid JSON and
fail output serialization.

Package code must:

- read required values with `job.input(name = "...")`;
- use named arguments for every `observer.*` call;
- validate process build identity before using build-specific signatures or
  offsets;
- reject zero, ambiguous, out-of-range, overflowing, or structurally invalid
  observations before conversion;
- bound scan matches, record counts, strides, pointer chains, arrays, and
  native allocations;
- return stable application error codes through
  `job.fail(code = "...", message = "...")`;
- keep user-facing failure messages useful but free of sensitive values;
- produce provenance-compatible source identity without guessing it.

## Source attempts and fallback

One source is the default. Multiple sources are permitted only when their exact
order and transition rule are part of `TASK.md`, `main.star`, and the output
schema.

Use:

```python
primary = job.attempt(
    source = "process-memory",
    function = read_from_memory,
)
if primary["ok"]:
    return finish(primary)

secondary = job.attempt(
    source = "save-file",
    function = discover_and_read_save,
)
```

`job.attempt` is not a general exception handler. It converts only:

- an application-level `job.fail` returned by that source function; and
- an Observer failure classified by the Host as failure of the selected
  observation source.

Protocol errors, invalid permissions, exhausted budgets, deadline expiry,
invalid native signatures, missing DLLs or exports, forged blobs, and Runner
or DLL crashes remain terminal infrastructure failures.
They must not activate another source.

Forbidden hidden fallback includes:

- choosing a file without an explicit, deterministic package-owned selection
  contract;
- selecting another process, module, save slot, artifact, DLL version, export,
  decoder, signature, offset set, or algorithm;
- substituting cached, placeholder, guessed, or partial data;
- treating malformed required data as an empty successful result;
- catching an invariant failure and continuing without recording the failed
  attempt.

If all explicitly declared sources fail at the application level, terminate
with one stable aggregate error. Do not return a schema-valid empty success.

## Observer usage

Observer operations are generic, finite, single-shot, and read-only. A Script
Package must never require memory writes, file writes, a watch loop, or a
long-lived Observer session.

Memory access must be bound to the exact Host-validated process identity.
Build-specific reads must first validate the executable identity expected by
the package. Prefer bounded batch or strided reads over repeated scalar calls,
while preserving readable invariants and correct byte accounting.

File access must remain below a package-declared logical root. Bounded
`observer.file.list` may provide metadata for a deterministic package-owned
selection policy; it never follows reparse points or returns file content.
Use `observer.file.open_blob` when a native DLL needs selected file content:

```python
listing = observer.file.list(
    path = {"root": "declared-root", "relative": "."},
    maxDepth = 3,
    maxEntries = 4096,
)
selected = select_one_file(listing)
blob = observer.file.open_blob(
    path = selected,
)
blob_path = native.blob_path(blob = blob["blob"])
```

The blob handle is valid only inside the current job. Do not copy large file
content through JSON, return the temporary blob path, or expose the original
private path.

## Native DLL contract

Native DLL support is package-owned generic FFI, not a provider registry.

The manifest declares only alias, package-relative artifact, platform, call limit, and
native-memory limit. `main.star` owns all export and ABI knowledge:

```python
library = native.load_library("save-decoder")
function = library.bind(
    name = "package_owned_export",
    parameters = [native.c_string(), native.out(native.handle())],
    result = native.i32(),
)
result = function.call(blob_path)
```

Starlark may load only a declared alias; it must not supply an arbitrary DLL
path. `native.load_library` is the API spelling because `load` is reserved
Starlark syntax.

Available observation-runtime V1 types are:

```text
native.void()
native.i32()
native.u32()
native.u64()
native.usize()
native.pointer()
native.handle()
native.c_string()
native.out(type)
native.struct(fields = [...])
native.array(type, count)
native.null()
```

For every native integration:

- document the artifact origin and reviewed build identity without embedding
  private source paths;
- declare the exact reviewed export names and signatures in Starlark;
- validate every return code and out value before use;
- use an explicit maximum before constructing `native.array`;
- release successful native handles on every application-level exit path;
- budget count-query, data-read, and cleanup calls in `maxCalls`;
- treat ABI mismatch, missing exports, library failure, or crash as terminal;
- keep game-specific structs and conversion logic out of Core.

The DLL is loaded directly into
`windows-observation-script-runner.exe`. A DLL crash can terminate the Runner
and the current job. Do not describe this boundary as crash isolation from the
Runner.

## Update and versioning workflow

After changing any declared member:

1. keep every regular member listed in `manifest.files`;
2. load the package through the real Go loader;
3. run the package and negative-path tests;
4. review the capability ID and version in runtime output.

Keep the capability ID in `rule.json` stable for the same task. Increment
`version` when the task behavior,
required inputs, output contract, permissions, source order, native ABI, or
artifact changes. Do not reuse a version to disguise a changed contract.

## Required tests

Every new package or material package change must provide evidence at four
levels.

### 1. Package loading

Add or update a Go test that calls `scriptpackage.Load` on the real package.
Include targeted negative tests for file declaration, path boundaries,
permission shape, or native declarations. Locally modified declared members
remain loadable without updating a digest.

### 2. Script behavior

Use a bounded fake Broker to test:

- successful output and schema validation;
- exact Observer call order and arguments;
- each application-level validation failure;
- approved source transition order, if any;
- terminal propagation of non-fallback-eligible Observer errors;
- input-schema failures, missing Host bindings, and exhausted limits;
- native call count, argument layout, return-code handling, and cleanup when
  native code is used.

Do not use production fallback, mock inventory, or placeholder data to make a
runtime path appear successful. Test fixtures stay inside tests.

### 3. Repository validation

Run from the repository root:

```bash
gofmt -w $(find cmd internal -name '*.go')
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observer.exe \
  ./cmd/windows-observer
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observation-script-runner.exe \
  ./cmd/windows-observation-script-runner
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -o .build/windows-observation-job.exe \
  ./cmd/windows-observation-job
```

Also run `git diff --check` and verify that package files are fully declared.

### 4. Signed-in Windows validation

Runtime behavior must be exercised in the signed-in interactive Windows
session with the exact built executables and package being reviewed. Validate:

- the expected process and explicit file binding;
- the actual source selected;
- record or result counts without publishing private records;
- ordered attempts and terminal failure behavior;
- capability ID, package version, and schema-valid output;
- Observer call/byte accounting;
- screen profile, fixed region, pixel accounting, and explicit no-evidence
  behavior when screen permission is used;
- native alias, completed call count, memory accounting, and terminal native
  failures when applicable;
- blob and temporary runtime cleanup.

Report only privacy-minimized evidence. Never commit or attach private save
files, memory dumps, item records, local account IDs, sensitive paths,
credentials, screenshots, or raw diagnostic logs.

## Review checklist

A package is ready only when all answers are yes:

- [ ] Does one leaf directory own exactly one finite task?
- [ ] Do `TASK.md`, `main.star`, `input.schema.json`, `output.schema.json`, and
      `manifest.json` describe the same inputs, sources, success, and failures?
- [ ] Is every regular file declared exactly once?
- [ ] Are Observer operations and byte/call limits least-privilege and finite?
- [ ] Are all process-build, pointer, size, count, and ABI assumptions checked?
- [ ] Is every fallback explicit, ordered, schema-visible, and approved?
- [ ] Do infrastructure failures terminate instead of changing source?
- [ ] Is every file explicitly selected rather than discovered implicitly?
- [ ] Does native code load only a manifest alias and release owned handles?
- [ ] Does successful output contain no private path or raw observed content
      beyond the task's deliberately public result contract?
- [ ] Do package, script, negative-path, cross-build, and live Windows tests
      pass?
- [ ] Does the public repository contain no private runtime artifact?

The maintained Crimson Desert inventory package is the current end-to-end
example:

- [`inventory/TASK.md`](../Rules/CrimsonDesert.exe/Actions/inventory/TASK.md)
- [`inventory/main.star`](../Rules/CrimsonDesert.exe/Actions/inventory/main.star)
- [`inventory/input.schema.json`](../Rules/CrimsonDesert.exe/Actions/inventory/input.schema.json)
- [`inventory/output.schema.json`](../Rules/CrimsonDesert.exe/Actions/inventory/output.schema.json)
- [`Crimson Desert inventory walkthrough`](examples/crimson-desert-inventory-job.md)

Architecture details remain in:

- [`Observation Script Package`](design/observation-script-package.md)
- [`Scripted observation job model`](design/observation-job-model.md)
- [`Script Runner native-library FFI`](design/native-library-ffi.md)
- [`Observation Job protocol usage`](protocol/observation-worker-v1-usage.md)
