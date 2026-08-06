# Observation Script Package

## Status

**Landed.**

An Observation Script Package is one `query` package below a Rule plugin's
`Modules/` directory. It has one finite Starlark entrypoint, human task
description, input and output schemas, Observer permissions, optional native
DLL artifacts, and explicit limits.

## Layout

```text
package/
|-- manifest.json
|-- TASK.md
|-- main.star
|-- input.schema.json
|-- output.schema.json
`-- native/
    `-- windows-amd64/
        `-- decoder.dll
```

Every regular file must appear exactly once in the `manifest.files` path list.
Directories are structural only. Undeclared files, symlinks, missing members,
and files larger than the package-member limit fail package loading. Local
plugin content is authoritative; no member digest is required.

## Manifest

```json
{
  "schemaVersion": 2,
  "version": 1,
  "title": "Read one value",
  "entrypoint": "main.star",
  "taskDocument": "TASK.md",
  "inputSchema": "input.schema.json",
  "outputSchema": "output.schema.json",
  "files": [
    "main.star",
    "TASK.md",
    "input.schema.json",
    "output.schema.json",
    "native/windows-amd64/decoder.dll"
  ],
  "permissions": {
    "memory": {
      "target": "rule/current-process",
      "operations": ["modules", "scan", "resolveRip", "readBatch", "readStrided"],
      "maxCalls": 12,
      "maxBytesRead": 536870912
    },
    "file": {
      "roots": [{
        "id": "declared-root",
        "resolver": {
          "kind": "windows-known-folder",
          "knownFolder": "LocalAppData",
          "relative": "Publisher/Game/save"
        }
      }],
      "operations": ["list", "openBlob"],
      "maxCalls": 2,
      "maxBytesRead": 67108864
    }
  },
  "nativeLibraries": {
    "decoder": {
      "platform": "windows-amd64",
      "artifact": "native/windows-amd64/decoder.dll",
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

`inputSchema` is the complete machine-readable input contract. The generic
launcher validates it before creating child processes, and the Script Runner
validates it again before Starlark execution. `rule/current-process` binds
memory access to the executable named by the owning
`Rules/<Executable.exe>/` folder; a package cannot name another executable.
Manifest file-root entries combine a logical alias with a supported portable
resolver. The Host resolves them locally; launcher callers cannot bind or
override an absolute path.

`nativeLibraries` is an executable boundary declaration, not a provider
registry. It contains no provider, version, operation, export, or ABI
knowledge. Those details belong to the package's Starlark.

The owning `rule.json` supplies the stable capability ID, package-relative
path, and runtime:

```json
{
  "modules": {
    "example/read": {
      "kind": "query",
      "path": "Modules/read",
      "runtime": "windows-observation-v1"
    }
  }
}
```

## Starlark surface

- `observer.memory.*` and `observer.file.*` exist only for manifest-declared
  operations.
- `native.load_library(alias)` loads only a declared package artifact.
- `native.blob_path(blob=...)` resolves only a blob issued in the current job.
- `library.bind(...)` and `function.call(...)` implement generic Windows FFI.
- `job.input`, `job.attempt`, and `job.fail` own finite task flow.

The runtime provides no network API, process launcher, environment access,
unbounded filesystem enumeration, polling, watch, timer, sleep, or hidden
source selection. A package may request bounded `file.list` and describe an
explicit source and file-selection policy, such as memory followed by the
newest unambiguous save under one declared root.

## Output and failure

`main(ctx)` receives only input accepted by the loaded input schema and must
return one JSON-compatible value accepted by the loaded output schema and
`maxResultBytes`. Package-load failures, input failures, invalid or
unresolvable known-folder declarations, native artifact failures,
platform mismatch, missing exports, invalid signatures, native limits,
deadline expiry, and schema failures are explicit. No missing artifact,
version, export, or source is substituted.

See [Script Runner native-library FFI](native-library-ffi.md) for ABI types and
[Scripted observation job model](observation-job-model.md) for process
lifecycle. Package authors and reviewers must also follow the
[`Script Package development contract`](../script-development-contract.md)
for authoring, fallback, privacy, versioning, and validation
requirements.
