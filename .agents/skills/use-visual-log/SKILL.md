---
name: use-visual-log
description: Operate and interpret WindowsAgent's independent visual-log process as an untrusted timeline index. Use when a supervising model needs to start or stop on-demand Gemma scene descriptions, check warm-up or producer status, retrieve visual-log events for a UTC time range, locate a likely game-recording interval, or decide which evidence range still needs authoritative review. Also use when diagnosing missing or low-quality visual-log samples without coupling them to evidence recording.
---

# Use Visual Log

Use the visual log only to find a likely time range. Treat every Gemma
description as an untrusted one-frame index entry; verify the selected interval
against the independent evidence layer before making a game-state claim.

## Preserve the module boundaries

- Keep evidence recording, visual logging, and high-level analysis on separate
  lifecycles.
- Start or stop only the visual-log run through its control API. Never start,
  stop, pause, delete, or reschedule evidence recording as a side effect.
- Do not synchronize the visual-log interval with the evidence recorder's 1 FPS
  cadence. Each visual-log tick reads the newest PC-local shared-memory frame
  newer than its own cursor; it never requests a screenshot or downloads an
  Evidence frame over HTTP.
- Do not ask Gemma to compare frames, identify START or END, detect events,
  interpret HUD state, infer actions, or write a gameplay narrative.
- Use the Evidence-provided `observedAt` or payload `timestamp`. Never use a
  timestamp generated or repaired by the model.
- Leave HUD interpretation and game semantics to the owning Action or
  authoritative evidence analysis.

## Read current configuration before operating

Read the matched game's `Rules/<Executable.exe>/VisualLog/config.json`. Obtain
the stream name, target executable, interval, frame-tap name, max-frame age,
prompt, model ID, and output event types from that file; do not assume Elite
Dangerous values for another game.

For operation-only work, do not edit the prompt, sampling, model, interval, or
Evidence max-frame age. Those are game configuration and development decisions.

Use the operator-provided control and event Bearer tokens without printing,
logging, committing, or copying them into the Rule. The default local surfaces
are:

```text
visual-log control  http://127.0.0.1:8789
event journal       http://127.0.0.1:8788
```

Treat configured or live values as authoritative when they differ.

## Start the on-demand run

1. Check `GET /healthz` on the visual-log control process.
2. Send authenticated `GET /v1/visual-log/status`.
3. If state is `warming`, `active`, or `stopping`, do not send a duplicate
   start request.
4. If logging is useful and no run is active, send authenticated
   `POST /v1/visual-log/runs` with `Content-Type: application/json` and the
   exact body `{}`.
5. Expect HTTP `201` and state `warming`. Poll status with a bounded deadline
   until it becomes `active`, `failed`, or the task no longer needs the log.
6. Record `sessionId`, `startedAt`, `lastSequence`, `droppedSamples`, and
   `lastDropStage`. State `active` proves only that warm-up ended and the loop
   is running; it does not prove that any useful description was committed.

The model needs warm-up, but a warm-up frame-tap read or model failure drops that
attempt and does not authorize a substitute model, old frame, prior
description, or direct screenshot request.

## Query a time range

Call the authenticated event-journal endpoint:

```text
GET /v1/events/range?from=<UTC>&to=<UTC>&stream=<stream>&after=<cursor>&limit=<count>
```

Apply these rules:

- Encode `from` and `to` as RFC3339 UTC timestamps ending in `Z`.
- Interpret the interval as half-open: `[from,to)`.
- Use the stream from the current game config; for the present Elite Dangerous
  config it is `visual-log`.
- Start with cursor `0` or omit `after`. Use a bounded `limit` from 1 through
  4096.
- Preserve global sequence order. If `complete` is `false`, request the same
  range again with `after=<nextCursor>` and continue until `complete` is true.
- Do not treat `lastSequence` as a visual-log sample count; it is the journal's
  global durable sequence.

Separate event types:

- `visual-log.observation`: read `payload.timestamp`,
  `payload.description`, `payload.untrusted`,
  `payload.evidence.captureId`, `payload.evidence.scheduledAt`, and model
  provenance. Require `untrusted: true`.
- `visual-log.failure`: note the failed sample and its Evidence provenance, then
  continue reading later entries. Do not turn it into an observation.

An invalid, missing, or poor Gemma answer means only that sample is absent. It
does not invalidate nearby evidence and must never terminate evidence
recording.

## Locate, then verify

Scan descriptions for coarse physical-scene cues such as an exterior space
view, station interior, large structure, planet, or star. Do not promote text
such as "approaching", "docking", "jumping", or HUD-derived claims into facts,
even if the model emits them.

Return one or more candidate UTC intervals with the supporting event sequences
and Evidence timestamps. Add context on both sides because the visual log and
evidence recorder are asynchronous and samples may be dropped. Choose the
padding from the actual task and observed sample spacing, not from an assumed
fixed synchronization rule.

Request the corresponding interval from the independent evidence process:

```text
GET /v1/evidence/range?from=<UTC>&to=<UTC>
```

Use the evidence process Bearer token, not the visual-log or event token. The
interval is half-open `[from,to)` and must fit the current Game config's
`maxRangeSeconds`. Read `manifest.json` first, preserve every explicit gap and
every `missingSlots` entry, and verify each listed MP4 segment's byte length and
SHA-256 before analysis. Segments may overlap the requested interval; use their
manifest timestamps to select the relevant seconds. The ZIP is the authority;
the replace-in-place Gemma frame tap is not an Evidence archive.

If `to` reaches the active uncommitted segment, HTTP 409
`EVIDENCE_RANGE_NOT_COMMITTED` is expected. Read `availableThrough` from
Evidence status, shorten the request or wait for segment commit, and retry. Do
not stop the recorder to force a commit.

If the visual log is empty, misleading, or unavailable, bypass it and request
the full relevant evidence range in bounded adjacent chunks. Never start,
stop, pause, or delete the evidence recorder while doing so; no such API is
part of its contract.

## Stop when no longer useful

Send authenticated `DELETE /v1/visual-log/runs/current` only when the current
high-level task no longer benefits from new index entries. Expect HTTP `200`
and state `stopping`, then use status to observe `stopped` or `failed` when
needed. Stopping the run affects only the visual logger.

Do not stop a run owned by another active task merely because one range query
completed. Use `sessionId` and current task context to establish ownership.

## Diagnose without architectural bypasses

- Rising `droppedSamples` or a recent `lastDropStage` indicates missing index
  entries, not lost evidence.
- `failed` with an event-append error means the visual logger cannot guarantee
  durable output. Leave evidence untouched and fall back to evidence analysis.
- HTTP `409 visual_log_already_active` means re-read status; do not restart the
  process.
- HTTP `409 event_cursor_ahead` means the requested cursor exceeds the current
  journal. Reconcile the stored cursor with `lastSequence`; do not guess data.
- Authentication, process, frame-tap publication, model, journal, domain interpretation, and
  evidence verification are separate acceptance layers. Report each one
  separately.

## Read deeper only when needed

- Read the [visual-log runtime contract](../../../docs/design/visual-log-runtime.md)
  before changing lifecycle, prompt, capture, or failure behavior.
- Read the [event-stream contract](../../../docs/design/event-stream-runtime.md)
  before changing range filtering, cursor semantics, or journal durability.
- Read the [evidence-recorder contract](../../../docs/design/evidence-recorder-runtime.md)
  before changing slot cadence, storage, range export, or failure behavior.
- Read the [repository runtime overview](../../../README.md) for current
  process flags, executable status, and public API surface.
