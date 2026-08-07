# Observation Job Protocol Usage

## Runtime

Build and install these observation components together:

```text
windows-observation-job.exe
windows-observation-script-runner.exe
windows-observer.exe
Rules/<Executable.exe>/Actions/
```

The Host launches only the Runner and Observer. All framed messages are
bounded, strict JSON over inherited pipes. No shell, PowerShell, polling,
watcher, HTTP observer, or native worker participates.

The launcher is capability-neutral. It resolves the capability through the
live Rule registry, requires `runtime: windows-observation-v1`, derives the
expected foreground executable from the owning Rule folder, validates the
package input schema, and locally resolves exactly the manifest-declared
file-root aliases.

One launch request has this generic shape:

```json
{
  "inputs": {}
}
```

## Package execution

The Host snapshots the currently registered Script Package for one job. The
Host and Runner independently validate and load that same job-scoped snapshot.
The Runner exposes:

```python
observer.memory.modules()
observer.memory.scan(...)
observer.memory.resolve_rip(...)
observer.memory.read_batch(...)
observer.memory.read_strided(...)
observer.file.list(...)
observer.file.stat(...)
observer.file.open_blob(path = ...)

native.load_library("manifest-alias")
native.blob_path(blob = blob_reference)
library.bind(name = "...", parameters = [...], result = ...)
function.call(...)

math.hypot(...)
math.atan2(...)
math.degrees(...)
math.round(...)

job.input(name = "...")
job.attempt(source = "...", function = ...)
job.fail(code = "...", message = "...")
```

`load_library` is the executable spelling because Starlark reserves `load`.
The alias resolves only to a validated package-relative DLL declared under
`nativeLibraries`. The Host never supplies or parses export names or ABI
signatures.

## Authorized save flow

```python
listing = observer.file.list(
    path = {"root": "manifest-root-alias", "relative": "."},
    maxDepth = 3,
    maxEntries = 4096,
)
selected = select_one_file(listing)
blob = observer.file.open_blob(path = selected)
path = native.blob_path(blob = blob["blob"])
library = native.load_library("save-decoder")
function = library.bind(
    name = "package_owned_export",
    parameters = [native.c_string(), native.out(native.handle())],
    result = native.i32(),
)
result = function.call(path)
```

`list` is manifest-permission-gated, depth/entry bounded, returns metadata
without file content, and never follows reparse points. Package Starlark owns
selection. `open_blob` copies exactly that selected file into the Host-created
job blob root and accounts the bytes. `blob_path` accepts only the opaque
handle issued for the same job. Large save bytes do not cross JSON.

## Provenance

The terminal result includes:

- every memory/file call and Observer accounting;
- native library load/bind/call phase records;
- alias, generic function name, calls used, native-memory bytes, result bytes,
  and error kind.

The result must not include private paths, save content, raw memory, or
unbounded diagnostics.

## Terminal behavior

Unknown capabilities or runtimes, invalid input, missing or extra resource
bindings, owning-Rule process mismatch, missing DLLs, platform mismatch,
missing exports, invalid FFI signatures, forged blobs, call/memory/result
limits, Runner crash, and deadline expiry fail explicitly. No alternative
artifact, version, export, decoder, save, or source is chosen.
