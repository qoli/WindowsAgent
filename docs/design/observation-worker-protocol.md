# Windows Observer Worker Protocol

## Status

**Landed.**

The worker implements bounded, finite, read-only memory, file, and primary
screen-region operations behind explicit permissions and accounting.

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
- `screen.readRegion`

The generic file backend also retains its explicitly declared read/hash
implementation where permitted. The inventory package uses bounded `list`
followed by `openBlob`.

`screen.readRegion` captures once without a cursor, requires the package to
state the expected primary-frame dimensions and an in-bounds rectangle, and
returns only that rectangle as packed RGB24 pixels. It revalidates the exact
foreground process identity after capture. It does not resize, search for a UI
element, interpret pixels, or substitute a different screen profile.

The Observer does not load DLLs, bind native exports, decode saves, know game
schemas or UI coordinates, poll, watch files, choose a file, or expose an HTTP
API. It returns bounded directory metadata or pixels; package Starlark owns
selection and interpretation. There is no legacy `file.decode`.

## Accounting and errors

The manifest supplies operation, call, byte, and screen-pixel budgets. Each
response records cumulative process bytes read, file bytes read, and screen
pixels read. Permission denial, malformed arguments, resolution mismatch,
budget exhaustion, deadline expiry, process identity drift, and file-root
escape fail explicitly. Application-level source read failures may be marked
fallback-eligible only when the package's documented task contract allows a
second source; protocol, permission, identity, and limit failures are terminal.

Native DLL behavior belongs to the
[Script Runner native-library FFI](native-library-ffi.md), never this worker.
