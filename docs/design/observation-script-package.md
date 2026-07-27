# Observation Script Package

## Status

**Landed.**

An Observation Script Package is a trusted local execution unit with one
finite Starlark entrypoint, human task description, output schema, Observer
permissions, optional native DLL artifacts, and explicit limits.

## Layout

```text
package/
|-- manifest.json
|-- TASK.md
|-- main.star
|-- output.schema.json
`-- native/
    `-- windows-amd64/
        `-- decoder.dll
```

Every regular file must appear in `manifest.files` with SHA-256. Directories
are structural only. Undeclared files, symlink escape, digest mismatch, missing
members, and files larger than the package-member limit fail package loading.
The package identity hashes the manifest and every declared file digest, so a
DLL change changes the package identity.

## Manifest

```json
{
  "schemaVersion": 1,
  "id": "example/read",
  "version": 1,
  "title": "Read one value",
  "entrypoint": "main.star",
  "taskDocument": "TASK.md",
  "outputSchema": "output.schema.json",
  "files": {
    "main.star": {"sha256": "..."},
    "TASK.md": {"sha256": "..."},
    "output.schema.json": {"sha256": "..."},
    "native/windows-amd64/decoder.dll": {"sha256": "..."}
  },
  "permissions": {
    "memory": {
      "target": "explicit/current-process",
      "operations": ["modules", "scan", "resolveRip", "readBatch", "readStrided"],
      "maxCalls": 12,
      "maxBytesRead": 536870912
    },
    "file": {
      "roots": ["explicit-root"],
      "operations": ["openBlob"],
      "maxCalls": 1,
      "maxBytesRead": 67108864
    }
  },
  "nativeLibraries": {
    "decoder": {
      "platform": "windows-amd64",
      "artifact": "native/windows-amd64/decoder.dll",
      "sha256": "...",
      "maxCalls": 4,
      "maxNativeMemoryBytes": 536870912
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

`nativeLibraries` is an integrity declaration, not a sandbox or provider
registry. It contains no provider, version, operation, export, or ABI
knowledge. Those details belong to the trusted package's Starlark.

## Starlark surface

- `observer.memory.*` and `observer.file.*` exist only for manifest-declared
  operations.
- `native.load_library(alias)` loads only a declared package artifact.
- `native.blob_path(blob=...)` resolves only a blob issued in the current job.
- `library.bind(...)` and `function.call(...)` implement generic Windows FFI.
- `job.input`, `job.attempt`, and `job.fail` own finite task flow.

The runtime provides no network API, process launcher, environment access,
filesystem enumeration, polling, watch, timer, sleep, or hidden source
selection. A package may describe an explicitly required source order, such as
memory followed by one user-selected save file.

## Output and failure

`main(ctx)` must return one JSON-compatible value accepted by the pinned output
schema and `maxResultBytes`. Package-load failures, native artifact failures,
platform mismatch, missing exports, invalid signatures, native limits,
deadline expiry, and schema failures are explicit. No missing artifact,
version, export, or source is substituted.

See [Script Runner native-library FFI](native-library-ffi.md) for ABI types and
[Scripted observation job model](observation-job-model.md) for process
lifecycle.
