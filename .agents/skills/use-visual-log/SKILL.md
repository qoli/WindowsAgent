---
name: use-visual-log
description: Operate WindowsAgent's independent finite Evidence recordings and visual-log process, and interpret Gemma output only as an untrusted timeline index. Use when a supervising model needs to request a bounded Evidence run, start or stop on-demand Gemma scene descriptions, correlate Action timestamps with recorded motion, retrieve or page visual-log events for a UTC range, locate a likely interval with a contact sheet, verify an Evidence ZIP and its MP4 manifest, or decide which recorded range still needs authoritative review.
---

# Use Visual Log

Use the visual log only to find a likely time range. Treat every Gemma
description as an untrusted one-frame index entry; verify the selected interval
against the independent evidence layer before making a game-state claim.

## Preserve the module boundaries

- Keep evidence recording, visual logging, and high-level analysis on separate
  lifecycles.
- Request Evidence explicitly through its own finite-run interface. Never
  start or extend Evidence as a side effect of starting Visual Log.
- Evidence has no extension, pause, manual-stop, or delete operation. A new run
  may be requested only after the previous run reaches a terminal state.
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
evidence control    http://127.0.0.1:8792
```

Treat configured or live values as authoritative when they differ.

## Request a finite Evidence run

Evidence recording is on demand. The process may be healthy while no WGC
session exists and no recording dot is visible. The default duration is 20
minutes; the hard maximum is one hour.

1. Check `GET /healthz` on the Evidence process.
2. Send authenticated `GET /v1/evidence/status` and read `state`, `finite`,
   `defaultDurationSeconds`, and `maxDurationSeconds`.
3. If state is `starting` or `recording`, do not submit another start. Record
   the existing `runId` and `endsAt` and decide whether its remaining window is
   sufficient.
4. Otherwise, send authenticated `POST /v1/evidence/runs` with
   `Content-Type: application/json`. Use `{}` for the default 1200-second run,
   or `{"durationSeconds":N}` for an explicit integer from 1 through 3600.
5. Expect HTTP 202. Require `finite:true`, a non-empty `runId`, the accepted
   `durationSeconds`, and immutable `requestedAt` and `endsAt`. Do not proceed
   on an ambiguous or unbounded response.
6. Follow the returned `Location`, or call
   `GET /v1/evidence/runs/<runId>`, until state becomes `recording`, `failed`,
   or the task no longer benefits from new Evidence. `starting` means only that
   the finite request was accepted; `recording` means WGC and the session-local
   recording-presence signal started.
7. Preserve `runId`, `requestedAt`, `startedAt`, `endsAt`, `frames`, `gaps`,
   `tapFailures`, and terminal state with the task evidence.

Before starting the high-level task, compare its plausible duration with the
immutable `endsAt`. If the remaining window is insufficient, state the coverage
limit before proceeding. Do not treat a later Visual Log observation, Action
event, or fresh screenshot as Evidence coverage beyond that deadline.

The run stops automatically at `endsAt` and finalizes its open MP4 segment.
State `completed` means finalization returned successfully; it does not prove
that every slot was a frame, so inspect `frames`, `gaps`, and the range
manifest. State `failed` is terminal for that run. Request a new run explicitly
if later Evidence is required; never assume continuous coverage between runs.

HTTP 409 `EVIDENCE_RUN_ACTIVE` includes `activeRun`. Use that run's `runId` and
deadline; do not retry in a loop or treat the conflict as an extension. Invalid
or over-3600 duration input is a caller error and must be corrected rather than
clamped.

## Start the on-demand Visual Log run

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
- When Action events already provide authoritative start, phase, or terminal
  timestamps, use those as the first range anchors. Visual Log then narrows the
  physical scene transition inside that bounded interval; it does not replace
  the Action timeline.
- Keep event queries narrow and paginate them. Project only sequence, type,
  timestamp, untrusted description, Evidence provenance, and model provenance
  for inspection. A terminal or shell tool truncating a large response is not
  proof that later events are absent; continue from the returned cursor or
  re-query a smaller interval.

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

When the Action event stream already identifies distinct phases, export small
adjacent Evidence ranges for those phases instead of one oversized recording.
For example, keep correction, transition, and terminal verification in
separate ranges when that makes the causal order clearer. Preserve their exact
half-open UTC boundaries so the ranges can be compared with Action cursors.

For a broad candidate interval, first request a bandwidth-light Evidence
contact sheet:

```text
POST /v1/evidence/contact-sheet
Content-Type: application/json

{"from":"<whole UTC second>","columns":4,"rows":4,"intervalSeconds":30}
```

The PC returns one `image/jpeg`. Cells are row-major: cell `i` is exactly
`from + i * intervalSeconds`. Read the embedded UTC timestamp and state on
every cell. `GAP` and `MISSING` are explicit Evidence states; never replace or
interpret them as nearby frames. The server accepts at most 8 columns, 8 rows,
64 cells, and a total span no greater than the Game Evidence config's
`maxRangeSeconds`.

Use coarse-to-fine queries when useful: first use a large interval to identify
a region, then submit a smaller interval around that region. A contact sheet is
decoded from committed MP4 data and never invokes WGC, but its reduced spatial
resolution and absent motion continuity make it a locator rather than final
proof.

Request the selected authoritative interval from the independent evidence
process:

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

Validate the exported package structurally before viewing it:

1. Require the manifest's `from` and `to` to equal the requested half-open
   interval, and retain `frameCount`, `gapCount`, and `missingCount`.
2. For each segment, read the nested video descriptor defined by the current
   manifest schema. Resolve exactly one packaged MP4 for its declared filename;
   archive paths may add a stable ordering prefix, so do not assume the manifest
   filename is a direct path. Reject zero or multiple matches.
3. Verify that MP4's declared byte length and SHA-256 before decoding it.
4. Use each segment record's `scheduledAt` to select frames inside the requested
   interval; overlapping segment boundaries are not extra requested coverage.
   Preserve foreground identity and every explicit `gap` or `missing` record.

Do not infer a clean timeline merely because the ZIP downloaded, every hash
matched, or aggregate `gapCount` is zero. Those prove package integrity and
declared slot coverage; inspect the relevant MP4 motion and terminal frame for
the domain claim.

If `to` reaches the active uncommitted segment, HTTP 409
`EVIDENCE_RANGE_NOT_COMMITTED` is expected. Read `availableThrough` from
Evidence status, shorten the request or wait for segment commit, and retry. Do
not stop the recorder to force a commit.

The same 409 applies when a contact-sheet cell reaches an active uncommitted
segment. A corrupt MP4, declared frame that cannot be decoded at its exact
timestamp, malformed grid, or excessive span is an explicit request failure;
do not retry with a screenshot, frame tap, Gemma image, or neighbouring second.

If the visual log is empty, misleading, or unavailable, bypass it and request
the full relevant recorded range in bounded adjacent chunks. Range reads do
not alter the active Evidence run. If the needed time was never covered by a
run, report the missing coverage; starting a new run cannot recover past
frames.

## Stop when no longer useful

Send authenticated `DELETE /v1/visual-log/runs/current` only when the current
high-level task no longer benefits from new index entries. Expect HTTP `200`
and state `stopping`, then use status to observe `stopped` or `failed` when
needed. Stopping the run affects only the visual logger.

Do not stop a run owned by another active task merely because one range query
completed. Use `sessionId` and current task context to establish ownership.

When a finite Evidence run reaches `completed` while its owned Visual Log is
still active, stop that Visual Log once no later index entry can help. New
`lastDropStage: evidence` samples after the immutable Evidence `endsAt` normally
mean there is no newer frame-tap source. Report them as an uncovered tail; do
not reinterpret them as gaps in the already committed Evidence interval.

## Diagnose without architectural bypasses

- Rising `droppedSamples` or a recent `lastDropStage` indicates missing index
  entries, not lost evidence.
- Compare every drop timestamp or status change with Evidence `endsAt` and
  `availableThrough`. Evidence-stage drops after completion do not retroactively
  invalidate earlier manifest-declared frames, gaps, or verified MP4 hashes.
- Evidence `starting` without transition to `recording` is a WGC startup or
  permission problem, not proof that frames exist. Inspect terminal
  `lastError` and do not substitute request-driven screenshots.
- Evidence `completed` with gaps or missing slots remains authoritative about
  those absences. Never fill them from the frame tap or Gemma output.
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
