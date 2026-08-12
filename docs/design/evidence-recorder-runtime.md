# Evidence Recorder Runtime

## Status

**Partially landed.** Persistent WGC capture, 1 FPS sampling, native H.264 MP4
segments, UTC manifests, authenticated range export, exact-timestamp contact
sheets, and a PC-local frame tap passed live Windows acceptance. Finite run
control and independent resident control-process installation also passed live
Windows acceptance. Retention remains deferred.

## Recording contract

`windows-evidence-recorder.exe` is the recording owner. Process start exposes
the authenticated control and read interface but leaves recording idle. An
accepted finite run creates one persistent WGC session in the signed-in PC
user's session, samples the newest frame at each whole UTC second, GPU-scales
and tone-maps it to 1920x1080, and writes H.264 MP4 segments through Media
Foundation. It never calls the request-driven screenshot API.

Every run is finite. `durationSeconds` is optional; omission selects 1200
seconds, and an explicit value must be an integer from 1 through 3600. The
default is 20 minutes and the hard maximum is one hour. The deadline begins
when the start request is accepted, so the interface can return the immutable
`endsAt` immediately. There is no renewal, extension, pause, manual-stop, or
delete operation. When the deadline expires, the recorder releases WGC, finalizes the
open segment, publishes terminal status, and returns to an idle process ready
to accept a new run.

The independent executable is installed as an interactive-user Scheduled Task
without its own trigger or restart policy. The external Watchdog keeps that
control process available. Process start or recovery is not recording activity:
only an authenticated finite-run request starts WGC and evidence capture.

Before starting the session, the process requests Windows borderless-capture
access and requires `IsBorderRequired=false` to read back exactly. Unsupported,
denied, canceled, timed-out, or mismatched permission state is a terminal
startup failure; the recorder does not silently retain the system capture
border. This setting applies only to the Recorder's persistent session. The
Capture Agent owns a separate persistent WGC worker generation and retains its
current Windows border behavior; neither process borrows or controls the
other's WGC session.

Each assigned second is exactly one of:

- a new frame accepted into the current video segment; or
- an explicit gap containing the failed stage and bounded error.

It never reuses an old frame, changes capture backend, or synthesizes a frame
for a gap. A frame-capture or foreground mismatch affects only that slot.
Failure to encode or durably commit the authoritative segment is terminal.

The recorder is independent of Gemma, Visual Log, the event journal, and
Actions. The high-level model may explicitly request a finite Evidence run,
but Visual Log startup has no implicit path to start, extend, pause, or
terminate it.

After the WGC session starts, the recorder holds the manual-reset named event
`Local\WindowsAgent.Evidence.Recording.v1` signaled for its exact stream
lifetime. The OSD may observe that presence to render one fixed yellow dot, but
the event exposes no control operation and the recorder never depends on the
OSD. When the recorder stops or exits, its handle closes; once the last
recorder handle is gone, the kernel object disappears and the OSD hides the dot
on its next one-second poll.

## Storage and frame tap

The recorder commits an MP4 and its JSON segment manifest atomically. The
manifest records every second represented by the segment, including frame and
gap entries, the half-open UTC bounds, Media Foundation format, byte length,
and SHA-256. A crash may leave an unreferenced `.partial.mp4`; it is never
treated as committed evidence.

After an Evidence frame is accepted by the segment encoder, the recorder also
publishes that newest 1080p BGRX frame to the configured
`Local\\WindowsAgent.Evidence.*.v1` named shared-memory mapping. This tap is a
replace-in-place, non-authoritative index input. A tap publication failure is
reported as `tapFailures` and `lastTapError` but does not stop or alter video
recording. Only PC-local readers can open it; it is not an HTTP frame service.

## Read interface

The process listens only on an explicit loopback address:

```text
GET /healthz
GET /v1/evidence/status
POST /v1/evidence/runs
GET /v1/evidence/runs/{runId}
GET /v1/evidence/range?from=<UTC>&to=<UTC>
POST /v1/evidence/contact-sheet
```

`POST /v1/evidence/runs` requires `Content-Type: application/json` and one
strict JSON object:

```json
{}
```

or:

```json
{"durationSeconds": 300}
```

A successful start returns HTTP 202. The response explicitly declares the
finite contract instead of requiring callers to infer it from documentation:

```json
{
  "state": "starting",
  "runId": "evr_...",
  "finite": true,
  "defaultDurationSeconds": 1200,
  "maxDurationSeconds": 3600,
  "durationSeconds": 1200,
  "requestedAt": "2026-08-12T10:00:00Z",
  "endsAt": "2026-08-12T10:20:00Z"
}
```

State changes to `recording` and adds `startedAt` only after the WGC session
and recording-presence signal have started. The terminal state is `completed`
after deadline cancellation and segment finalization, or `failed` with
`lastError`. `GET /v1/evidence/runs/{runId}` exposes retained in-process status
for recent runs. `GET /v1/evidence/status` returns the latest run status plus
`availableThrough` when committed evidence exists. An idle status still
returns `finite:true`, `defaultDurationSeconds`, and `maxDurationSeconds` so a
caller can discover the finite constraint before starting.

Zero, negative, fractional, null, unknown-field, and over-3600 duration inputs
return HTTP 400. A second start while state is `starting` or `recording`
returns HTTP 409 `EVIDENCE_RUN_ACTIVE` and includes `activeRun` with its
`runId` and `endsAt`; it never extends or replaces that run.

Every route except health requires the Evidence Bearer token. Ranges are
half-open `[from,to)`, use RFC3339 UTC timestamps ending in `Z`, and cannot
exceed `maxRangeSeconds`. A request that reaches into the active uncommitted
segment returns HTTP 409 `EVIDENCE_RANGE_NOT_COMMITTED`; the caller may retry
using `availableThrough` from status.

A successful request returns a ZIP containing `manifest.json` and every
complete committed MP4 segment overlapping the range. The manifest counts
in-range frames and gaps and explicitly enumerates `missingSlots`. Segments are
not silently clipped or omitted. Their byte lengths and SHA-256 values are
rechecked while building the ZIP; corruption fails the request.

### Contact sheet

The contact-sheet route accepts one strict JSON request:

```json
{
  "from": "2026-08-12T12:00:00Z",
  "columns": 4,
  "rows": 4,
  "intervalSeconds": 10
}
```

`from` must be a whole UTC second. Cells are assigned in row-major order and
cell `i` represents `from + i * intervalSeconds`. Columns and rows are each
bounded to 1-8, the grid is bounded to 64 cells, and its total evidence span
must fit `maxRangeSeconds`. A successful response is one `image/jpeg` with a
480x270 thumbnail plus a 30-pixel timestamp footer per cell.

The recorder resolves every cell through committed segment manifests, verifies
the selected MP4 hashes, and decodes each selected segment from its beginning.
Only a Media Foundation sample whose timestamp exactly equals the requested
slot is accepted. A manifest `gap` or absent slot is rendered as a labelled
`GAP` or `MISSING` cell. A missing declared frame, corrupt MP4, decode error,
unexpected timestamp, or duplicate decoder output fails the whole request;
nearby frames are never substituted.

The JPEG is derived from authoritative Evidence but loses resolution and video
continuity. Its purpose is hierarchical timeline location: request a coarse
grid, request a finer grid around a candidate, then retrieve the exact MP4
range for authoritative analysis. Contact-sheet work has no recording-control
path and does not use WGC, the frame tap, Visual Log, or Gemma.

## Live acceptance

On 2026-08-11 the GUI-subsystem recorder ran in an isolated interactive-user
Scheduled Task with `EliteDangerous64.exe` foreground on a 3840x2160 HDR
display. The final persistent-WGC implementation recorded 24 consecutive
samples with zero gaps. The downloaded range contained 6-frame and 10-frame
H.264 segments; `ffprobe` verified 1920x1080, 1 FPS, and exact 6/10 second
durations. A decoded frame verified orientation, GPU HDR tone mapping, and the
actual Elite Dangerous scene.

During the later frame-tap/Gemma integration run, Evidence advanced from 5 to
9 frames with zero gaps and zero tap failures while Visual Log performed model
inference. The pre-existing capture Agent and event stream retained their PIDs,
proving this recorder did not depend on or disrupt screenshot requests.

On 2026-08-12 the final contact-sheet artifact decoded eight consecutive
committed Elite Dangerous samples into a labelled 4x2, 1920x600 JPEG in 997 ms.
The decoder accepted Media Foundation's 1920x1088 H.264 storage surface only
after validating its explicit 1920x1080 minimum display aperture; the rendered
frames were visually checked for correct orientation and row-major UTC labels.
An invalid grid returned HTTP 400 and a cell reaching the active segment
returned HTTP 409. Evidence advanced from 14 to 17 frames during the bounded
run with zero gaps and zero frame-tap failures. The isolated task, listener,
token, and private video data were removed; the existing Capture and Event
processes retained PIDs 15032 and 21072.

Later on 2026-08-12, isolated PC runs on Windows build 26100 verified
borderless permission and `IsBorderRequired=false`. The exact final Recorder
artifact reached three advancing frames with zero gaps and zero frame-tap
failures while the exact final OSD artifact displayed the new fixed yellow dot.
Stopping the Recorder task without a graceful in-process stop removed the dot
within the next poll while the isolated OSD remained running. The isolated
tasks, token, binaries, and private recordings were then removed; the installed
Capture Agent, Event Stream, and production OSD retained their original
processes.

The initial finite-run refactor was accepted on the same Windows build with
the game foreground. At that version, process startup returned `idle`,
`finite:true`, and both duration limits at 1200 without opening WGC or showing
the OSD dot. An explicit
15-second run returned HTTP 202 with an immutable 15-second deadline, moved
from `starting` to `recording`, rejected a concurrent start with HTTP 409, and
completed 28 milliseconds after its deadline while the control process and
listener remained alive. It committed 15 frames, zero gaps, zero tap failures,
and zero missing slots across 5-second and 10-second 1920x1080 H.264 segments;
both reported 1 FPS and exact matching durations. The yellow OSD dot appeared
only during recording and disappeared after automatic completion. An
over-limit request returned HTTP 400, and a later `{}` start returned an exact
1200-second deadline. The isolated tasks, listener, token, binaries, and
private recordings were removed; the pre-existing Capture Agent, Event Stream,
and production OSD retained their original processes.

The one-hour limit update was then accepted with the final Recorder artifact in
another isolated interactive-user task. Idle status advertised a 1200-second
default and 3600-second maximum. An omitted duration produced an exact
1200-second deadline, an explicit 3600 produced an exact 3600-second deadline,
and 3601 returned HTTP 400. Both accepted starts entered `recording`; the first
then committed explicit foreground-mismatch gaps because the target game was
not foreground, so no valid video frame is claimed by this API-boundary test.
The isolated task used its own frame tap and was removed with its token,
binaries, and private data. The pre-existing production Recorder retained its
process and listener throughout.

No automatic retention or deletion policy exists. Runtime video, manifests,
tokens, and logs are private operator data outside the public repository.
