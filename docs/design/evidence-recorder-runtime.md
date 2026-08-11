# Evidence Recorder Runtime

## Status

**Partially landed.** Persistent WGC capture, 1 FPS sampling, native H.264 MP4
segments, UTC manifests, authenticated range export, and a PC-local frame tap
passed live Windows acceptance. Persistent installation and retention remain
deferred.

## Recording contract

`windows-evidence-recorder.exe` is the recording owner. On process start it
creates one persistent WGC session in the signed-in PC user's session, samples
the newest frame at each whole UTC second, GPU-scales and tone-maps it to
1920x1080, and writes H.264 MP4 segments through Media Foundation. It never
calls the request-driven screenshot API.

Each assigned second is exactly one of:

- a new frame accepted into the current video segment; or
- an explicit gap containing the failed stage and bounded error.

It never reuses an old frame, changes capture backend, or synthesizes a frame
for a gap. A frame-capture or foreground mismatch affects only that slot.
Failure to encode or durably commit the authoritative segment is terminal.

The recorder is independent of Gemma, Visual Log, the event journal, Actions,
and the high-level model. Those consumers cannot pause or terminate recording.

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

## Range API

The process listens only on an explicit loopback address:

```text
GET /healthz
GET /v1/evidence/status
GET /v1/evidence/range?from=<UTC>&to=<UTC>
```

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

No automatic retention or deletion policy exists. Runtime video, manifests,
tokens, and logs are private operator data outside the public repository.
