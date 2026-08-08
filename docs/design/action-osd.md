# Action OSD

## Status

**Landed.** The maintained companion, explicit activity contract, bounded
startup reconstruction, installer, lifecycle expiry, and live event client are
implemented and tested. Real-device acceptance used
`elite-dangerous/select-contacts-panel` over a live 4K HDR game: the overlay
showed `LIVE`, elapsed time, the Action ID, and committed activity while the
Action ran; `DONE` then disappeared automatically. The installed production
task excludes the OSD from capture.

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

## Display

While an Action is running, the overlay shows a pulsing red dot, `LIVE`, elapsed
time, the canonical current Action ID, and the latest three distinct activity
records from oldest to newest. The newest record is visually strongest. A
running Action remains visible even when no new activity has arrived.

Terminal presentation is `DONE`, `STOPPED`, or `FAILED`. Successful and stopped
Actions remain for three seconds; failed Actions remain for eight seconds.

The window is topmost, layered, click-through, tool-only, and non-activating.
It follows the foreground monitor and is excluded from capture by default.

## Process boundary

`windows-action-osd.exe` is an independent GUI-subsystem companion in the
signed-in interactive session. It reads the authenticated loopback event API.
It starts from the journal's current durable cursor, so historical completed
Actions do not replay onto the desktop. An event-stream disconnect or invalid
activity is fatal and visible in the OSD process log; no alternate log or event
source is attempted.
