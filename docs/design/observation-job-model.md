# Scripted Observation Job Model

## Status

**Landed as the generic `windows-observation-v1` Starlark launcher.**

One launch invocation resolves any uniquely registered capability, validates
its package and request, runs one finite job, and produces one terminal
schema-valid JSON result or one typed failure. The invocation may come from
the unauthenticated WindowsAgent HTTP endpoint or the local launcher CLI;
both execute the same local job command in the signed-in session.

## Process model

```text
windows-observation-job.exe
  |-- windows-observation-script-runner.exe --package-root <job snapshot>
  `-- windows-observer.exe (only when a permission namespace is declared)
```

The Go launcher uses `CreateProcessW`, `CREATE_NO_WINDOW`, restricted inherited
handles, and one Windows Job Object with active-process, per-process-memory,
total-memory, deadline, and kill-on-close limits. It never launches
PowerShell, `cmd.exe`, a native-extension worker, a polling loop, watcher, or
HTTP observer.

A package with `memory`, `file`, and `screen` all set to `null` is a pure
input-to-output Action. Its job limit permits only the Script Runner child and
does not launch or initialize an Observer. If that Runner nevertheless emits a
`broker/call`, the Host fails the job explicitly. Packages declaring any
Observer namespace retain the two-child model above.

The launcher contains no game or capability allowlist. It derives the expected
foreground executable from the capability's owning Rule folder, accepts only
the runtime declared by `rule.json`, validates `inputs` against the package
input schema, and resolves each manifest file-root declaration through its
supported local Windows known-folder resolver. HTTP and CLI callers cannot
provide absolute Host paths.

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
and error kind. The Host validates those values against the job-scoped package
limits but does not parse provider-specific exports or signatures.

## Blob lifecycle

The Host creates one temporary blob root. The Observer copies only the
explicitly authorized file selected by the job, accounts its bytes, and
returns a random 256-bit handle. The Host records handle and size before
returning the result to Starlark. `native.blob_path` accepts only that exact
same-job handle. The temporary root is removed after job processes exit.

## Generic launch request

The trusted caller supplies one absolute strict-JSON request file:

```json
{
  "inputs": {}
}
```

`inputs` is package-defined and schema-validated. File-root aliases, known
folder identity, and package-relative root paths are declared by the package;
the Host resolves them locally. Machine-specific absolute paths never belong
in the request or distributable Rule plugin.

See [Observation Script Package](observation-script-package.md) and
[Script Runner native-library FFI](native-library-ffi.md).
