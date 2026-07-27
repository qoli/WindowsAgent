# Scripted Observation Job Model

## Status

**Landed for the registered Crimson Desert inventory job.**

One command invocation runs one finite job and produces one terminal
schema-valid JSON result or one typed failure.

## Process model

```text
windows-observation-job.exe
  |-- windows-observation-script-runner.exe --package-root <verified package>
  `-- windows-observer.exe
```

The Go launcher uses `CreateProcessW`, `CREATE_NO_WINDOW`, restricted inherited
handles, and one Windows Job Object with active-process, per-process-memory,
total-memory, deadline, and kill-on-close limits. It never launches
PowerShell, `cmd.exe`, a native-extension worker, a polling loop, watcher, or
HTTP observer.

The Script Runner is the trusted package execution process. If a package loads
a native DLL, that DLL lives in the Runner process. A DLL crash may terminate
the Runner and becomes typed `SCRIPT_PROCESS_FAILED` for the current job.

## Broker messages

The Runner and Host use bounded framed JSON:

- `broker/call` forwards one authorized memory/file operation to the Observer;
- `broker/blobPath` resolves one same-job opaque blob reference;
- `broker/nativeRecord` records generic native load/bind/call accounting.

There is no `extension/call` method or compatibility route.

Observer provenance records call ID, namespace, operation, observation time,
and cumulative Observer accounting. Native provenance records alias, generic
action, function name, phase, calls used, native-memory bytes, result bytes,
and error kind. The Host validates those values against the verified package
limits but does not parse provider-specific exports or signatures.

## Blob lifecycle

The Host creates one temporary blob root. The Observer copies only the
explicitly authorized file selected by the job, accounts its bytes, and
returns a random 256-bit handle. The Host records handle and size before
returning the result to Starlark. `native.blob_path` accepts only that exact
same-job handle. The temporary root is removed after job processes exit.

## Crimson inventory registration

The current command resolves one registered package and one explicit process
identity plus save root/relative path. The package itself owns the source order
and Crimson ABI. This command-level registration is the only permitted
Crimson-specific exception in the Job command; `internal/observationjob`
remains provider-neutral.

See [Observation Script Package](observation-script-package.md) and
[Script Runner native-library FFI](native-library-ffi.md).
