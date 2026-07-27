# Script Runner Native-Library FFI

## Status

**Landed.**

Script Packages are trusted local execution units. A package may own a native
DLL artifact, its ABI declarations, and its call logic. WindowsAgent Core owns
only package-integrity verification, generic Windows amd64 FFI, bounded
execution, job-scoped blob resolution, and provenance.

There is no Native Extension Registry, provider adapter, extension broker, or
separate native worker.

## Process boundary

```text
windows-observation-job.exe
  |-- windows-observation-script-runner.exe
  |     `-- manifest-declared package DLL
  `-- windows-observer.exe
```

Both children are launched directly with `CreateProcessW` and
`CREATE_NO_WINDOW`, then assigned to the same Windows Job Object. DLL failure
may terminate the Script Runner and therefore the current job. No PowerShell,
`cmd.exe`, polling loop, file watcher, HTTP observer, or third worker is used.

## Manifest contract

```json
{
  "files": {
    "native/windows-amd64/decoder.dll": {
      "sha256": "<64 lowercase hex>"
    }
  },
  "nativeLibraries": {
    "save-decoder": {
      "platform": "windows-amd64",
      "artifact": "native/windows-amd64/decoder.dll",
      "sha256": "<same digest>",
      "maxCalls": 4,
      "maxNativeMemoryBytes": 536870912
    }
  }
}
```

The artifact is package-relative, must also appear in `files`, and therefore
contributes to the package digest. Missing files, digest mismatches, undeclared
files, absolute paths, traversal, symlink escape, and platform mismatch fail
explicitly. No alternate DLL or version is selected.

Starlark identifies the DLL only by alias:

```python
library = native.load_library("save-decoder")
```

Starlark reserves `load` as syntax, including after a dot, so the executable
API is named `load_library`; WindowsAgent does not rewrite package source.

## Generic API

```python
library.bind(name = "...", parameters = [...], result = ...)
function.call(...)
native.blob_path(blob = blob_reference)

native.void()
native.i32()
native.u32()
native.u64()
native.usize()
native.pointer()
native.handle()
native.c_string()
native.out(type)
native.struct(fields = [{"name": "...", "type": type}])
native.array(type, count)
native.null()
```

`function.call` accepts only positional input arguments. Parameters declared
with `native.out(type)` are allocated by the Runner and omitted from the input
argument list. Every call returns:

```json
{
  "result": "<declared scalar or null for void>",
  "out": ["<decoded out values in declaration order>"]
}
```

The Windows backend uses `x/sys/windows`, `unsafe`, `LoadLibraryW`,
`GetProcAddress`, and the Windows x64 calling convention without CGO. The
generic type layer calculates natural V1 size/alignment, allocates
null-terminated UTF-8 strings and bounded native buffers, decodes structs and
arrays, checks the context before and after calls, and releases the loaded DLL
when the Script run ends.

## Limits and failures

Each library has cumulative `maxCalls` and `maxNativeMemoryBytes` accounting.
Decoded call output is also bounded by the Script Package
`limits.maxResultBytes`. Representative terminal codes are:

- `NATIVE_LIBRARY_NOT_DECLARED`
- `NATIVE_PLATFORM_MISMATCH`
- `NATIVE_LIBRARY_LOAD_FAILED`
- `NATIVE_EXPORT_NOT_FOUND`
- `NATIVE_SIGNATURE_INVALID`
- `NATIVE_ARGUMENT_INVALID`
- `NATIVE_CALL_LIMIT_EXCEEDED`
- `NATIVE_MEMORY_LIMIT_EXCEEDED`
- `NATIVE_RESULT_LIMIT_EXCEEDED`
- `NATIVE_BLOB_INVALID`
- `NATIVE_DEADLINE_EXCEEDED`

Missing or invalid state never triggers another DLL, version, export, or
decoder.

## Blob path

`observer.file.open_blob` performs the authorized one-shot read and returns an
opaque handle. `native.blob_path` sends that reference to the Job Host, which
accepts it only if the same job's Observer issued it and the temporary file
still matches the registered size. The returned temporary path is for the
trusted package's current call; the original user path is not exposed.

The Job Host records generic native load, bind, and call provenance, including
alias, function name, phase, call count, native-memory bytes, result bytes, and
error kind. It does not parse the DLL exports or understand their ABI.
