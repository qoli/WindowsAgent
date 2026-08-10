# Action OSD

## Status

**Landed.** The maintained companion, explicit activity contract, Ephemeral
Action Sequence projection, bounded startup reconstruction, installer,
lifecycle expiry, and live event client are implemented and tested. Real-device acceptance used
`elite-dangerous/select-contacts-panel` over a live 4K HDR game. The installed
production task excludes the OSD from capture. The original card presentation
was subsequently replaced by the landed compact viewfinder presentation.

## Responsibility

The Action OSD is a display-only projection of the existing `action.runs`
stream. It never invokes, stops, retries, or changes an Action. A missing or
failed OSD does not alter Action execution.

The Host owns lifecycle presentation from `action.started`,
`action.completed`, `action.failed`, and `action.cancelled`. A Streaming Action
owns its human-readable activity semantics and publishes them explicitly with:

```python
stream.activity(message="Throttle set to 100%", level="info")
```

`message` is one canonical line of at most 160 Unicode characters. `level` is
exactly `info`, `warning`, or `error`. Invalid activity fails the Streaming
Action call explicitly. The OSD does not infer presentation from arbitrary
domain payloads and does not tail process logs as a substitute.

An Ephemeral Action Sequence remains one OSD session and one correlation
chain. `action.sequence.step.started` selects the current child Action and adds
the Host-owned `Step n/total` record. The OSD unwraps only a schema-valid
`action.activity` from `action.sequence.child.event`; arbitrary child domain
payloads are not converted into display text. Child output and step completion
advance validation without creating another OSD session. Only the parent
`action.completed`, `action.failed`, or `action.cancelled` sets terminal
presentation. Missing, out-of-order, unknown, or provenance-mismatched Sequence
events fail the OSD stream explicitly.

## Display

The overlay occupies a compact transparent region at the top-left of the
foreground monitor. It has no panel, card, border, status label, elapsed time,
or per-record timestamps. While an Action is running it shows only a fixed-size
red dot that alternates every 1000 milliseconds between fully visible and fully
absent, the short current Action name (the final segment of its canonical ID),
and at most three distinct activity records from oldest to newest. During a
Sequence the short name is the active child Action rather than
`ephemeral-action-sequence`; each new step clears the previous child's records
and begins with `Step n/total`. There is no
fade, opacity ramp, size change, or color change. The full canonical ID remains
in the event model. The newest record is
visually strongest. Text and the status dot are flat colors with no outline,
shadow, contrast plate, or readability fallback; insufficient contrast against
the foreground content is accepted. A running Action remains visible even when
no new activity has arrived.

Terminal presentation is `DONE`, `STOPPED`, or `FAILED`. Successful and stopped
Actions remain for three seconds; failed Actions remain for eight seconds. The
compact presentation does not render those status words: a static green, grey,
or red dot communicates the respective terminal state until expiry.

The window is topmost, layered, click-through, tool-only, and non-activating.
It follows the foreground monitor and is excluded from capture by default.

## Process boundary

`windows-action-osd.exe` is an independent GUI-subsystem companion in the
signed-in interactive session. It reads the authenticated loopback event API.
It starts from the journal's current durable cursor, so historical completed
Actions do not replay onto the desktop. An event-stream disconnect or invalid
activity is fatal and visible in the OSD process log; no alternate log or event
source is attempted.

An operator may install the OSD with an explicit `minimum-event-cursor` when a
deployed journal contains an older, intentionally unsupported event contract.
Startup replay begins after that exact durable boundary while still replaying
all later events. A cursor beyond the journal's current last sequence fails;
the OSD never detects, repairs, or silently skips malformed history itself.
