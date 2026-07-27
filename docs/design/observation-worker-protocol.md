# Windows Observer Worker Protocol

## Status

**Partially landed.**

The current worker implements the bounded, finite, read-only operations needed
by the registered inventory job. Additional generic observation operations may
be added later behind explicit permissions and accounting.

## Boundary

`windows-observer.exe` is a game-neutral child process. It accepts strict,
length-prefixed JSON messages on inherited stdin/stdout:

```text
initialize
observer/call
shutdown
```

The first request binds one job ID, deadline, exact process identity, logical
file roots, permissions, and Host-created blob root. Every call is one-shot.

Supported capability surface:

- `memory.modules`
- `memory.scan`
- `memory.resolveRip`
- `memory.readBatch`
- `memory.readStrided`
- `file.list`
- `file.stat`
- `file.openBlob`

The generic file backend also retains its explicitly declared read/hash
implementation where permitted. The inventory package uses bounded `list`
followed by `openBlob`.

The Observer does not load DLLs, bind native exports, decode saves, know game
schemas, poll, watch files, choose a file, or expose an HTTP API. It returns
bounded directory metadata; package Starlark owns selection. There is no
legacy `file.decode`.

## Accounting and errors

The manifest supplies operation, call, and byte budgets. Each response records
cumulative process bytes read and file bytes read. Permission denial, malformed
arguments, budget exhaustion, deadline expiry, process identity drift, and
file-root escape fail explicitly. Application-level source read failures may
be marked fallback-eligible only when the package's documented task contract
allows a second source; protocol, permission, identity, and limit failures are
terminal.

Native DLL behavior belongs to the
[Script Runner native-library FFI](native-library-ffi.md), never this worker.
