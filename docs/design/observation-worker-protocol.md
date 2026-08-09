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
- `file.readJson`
- `file.openBlob`
- `screen.readRegion`

The generic file backend also retains its explicitly declared raw read/hash
implementation where permitted. `readJson` performs one size-bounded,
duplicate-key-rejecting object read and returns relative source identity plus
file/source timing evidence. The inventory package uses bounded `list`
followed by `openBlob`.

`screen.readRegion` captures once without a cursor. The package supplies one
rectangle in the fixed 1920x1080 reference coordinate space and explicitly
chooses `reference` or `native` sampling. Core maps the rectangle through the
centered 16:9 viewport. Reference sampling returns exactly the requested
reference dimensions; native sampling preserves the mapped physical pixel
density.

The WGC backend copies only the mapped physical region into a small GPU
texture. A compute shader performs bilinear reference resizing when requested,
HDR tone mapping, and RGB24 packing before the bounded output texture is read
back. The path does not allocate, encode, decode, or tone-map a complete
primary-monitor image. It revalidates the exact foreground process identity
after capture and does not search for or interpret a UI element.
The region path requires D3D11 compute shader model 5.0 and the Windows
`d3dcompiler_47.dll`. Shader compilation or device support failure is terminal;
there is no full-frame CPU fallback.

The Observer does not load DLLs, bind native exports, decode saves, know game
schemas or UI coordinates, poll, watch files, choose a file, or expose an HTTP
API. It returns bounded directory metadata or pixels; package Starlark owns
selection and interpretation. There is no legacy `file.decode`.

## Accounting and errors

The manifest supplies operation, call, byte, and screen-pixel budgets. Each
response records cumulative process bytes read, file bytes read, and returned
screen pixels. A screen permission authorizes exactly one capture; a future
multi-region operation must be an atomic protocol addition. Permission denial,
malformed reference coordinates, unknown sampling, mapped pixel-budget
exhaustion, shader or capture failure, deadline expiry, process identity drift,
and file-root escape fail explicitly. Application-level source read failures may be marked
fallback-eligible only when the package's documented task contract allows a
second source; protocol, permission, identity, and limit failures are terminal.

Native DLL behavior belongs to the
[Script Runner native-library FFI](native-library-ffi.md), never this worker.
