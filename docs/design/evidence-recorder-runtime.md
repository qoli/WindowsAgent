# Evidence Recorder Runtime

## Status

**Partially landed.** The independent process, strict per-game configuration,
one-second slot scheduler, durable frame/gap records, authenticated status API,
and integrity-checked range ZIP are implemented and passed bounded live Windows
acceptance. Persistent installer and retention policy remain deferred.

## Contract

`windows-evidence-recorder.exe` starts recording immediately and has no start,
stop, pause, or delete HTTP route. It is independent of Gemma, the visual log,
the event journal, and Actions. Those modules cannot determine whether this
process remains alive.

Each whole UTC second is one timeline slot. The process commits either:

- a verified 1920x1080 JPEG with capture identity, observation time, byte
  length, and SHA-256; or
- an explicit gap with the failed stage and bounded error.

A capture timeout, busy capture Agent, foreground mismatch, or capture artifact
failure commits only that slot as a gap and continues. It never retries into a
later slot, reuses an older image, changes profile, or asks another provider.
Failure to durably commit the frame or gap terminates the recorder because the
timeline can no longer be asserted.

The capture scheduler is not synchronized with visual-log sampling. Sub-second
completion lateness starts the next assigned slot immediately but never issues
more than one capture for that slot. Once capture is a complete interval behind,
the actually missed second is recorded as a `scheduler_overrun` gap; the
recorder does not issue a multi-frame catch-up burst.

## Range API

The process listens only on an explicit loopback address:

```text
GET /healthz
GET /v1/evidence/status
GET /v1/evidence/range?from=<UTC>&to=<UTC>
```

Status and range require the exact Bearer token; health is unauthenticated.
Ranges are half-open `[from,to)`, require RFC3339 UTC timestamps ending in
`Z`, and must not exceed the configured `maxRangeSeconds`. A successful range
is a ZIP containing `manifest.json` and `frames/*.jpg`. The manifest preserves
every committed frame and gap in order and records `snapshotAt`, counts, and
the requested bounds. `missingSlots` and `missingCount` explicitly enumerate
whole-second slots for which no durable record exists, including recorder
downtime; absence is never presented as a complete timeline. Empty ranges
remain valid explicit manifests.

The server builds the complete ZIP and rechecks every JPEG byte length and
SHA-256 before sending HTTP success. Missing, corrupted, duplicated, or
malformed evidence fails the request explicitly; it is never silently omitted.

## Storage and privacy

Metadata files are the atomic visibility boundary. A JPEG is durably renamed
before its matching JSON record appears. A crash can leave an unreferenced JPEG,
but range reads ignore it rather than inventing a timeline entry.

No automatic retention or deletion policy is implemented yet. The evidence
directory contains private screenshots and must remain operator-owned runtime
data outside the public repository.

## Live acceptance

The 2026-08-11 acceptance ran the GUI-subsystem recorder in an isolated
interactive-user Scheduled Task against a fresh matched
`EliteDangerous64.exe` foreground. After correcting an initial console-window
foreground violation and a sub-second scheduler-boundary error, the exact final
artifact committed 17 frames across 17 assigned seconds with zero gaps.

The authenticated range route returned a complete ZIP; an independent local
read of the exact final artifact verified all 16 included JPEG byte lengths,
SHA-256 values, and ZIP CRCs. Its deliberately padded range explicitly listed
six pre-start recorder-downtime slots. An earlier cross-restart range preserved
committed gaps and downtime separately. Missing authentication returned HTTP
401 and a range above the configured hour returned HTTP 413.

A separate bounded failure-injection process accumulated four capture gaps and
remained `recording`. During a real capture-Agent outage the primary recorder
also remained alive, committed the connection failures as gaps, and resumed
frames after capture health returned. Recorder restart changed its PID and the
same data directory remained range-readable across the downtime.

The acceptance Tasks, private PC evidence, and token were removed after the
verified ZIP was copied to ignored local diagnostics. The production capture
Agent and event stream remained healthy; no persistent evidence installation
was left behind because retention is not yet defined.
