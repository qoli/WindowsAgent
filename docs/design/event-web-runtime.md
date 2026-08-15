# Event Web Runtime

## Status

**Landed.** The read-only Web projection module, embedded browser UI, strict
HTTP interface, independent Windows executable, GUI-subsystem verification,
installer, and focused tests are implemented and accepted in a signed-in
Windows session and an existing authenticated Arc browser session.

## Responsibility

`windows-event-web.exe` projects the authenticated loopback event journal and
the existing Action OSD model into a browser. It is an independent background
process. It does not create a window, console, tray icon, Action, event,
recording, capture, or input operation. Its failure changes none of those
runtimes.

The executable is linked as PE Windows GUI subsystem 2 solely to suppress a
console window. It contains no GUI event loop. Because the exact OSD mirror
reads the session-local recent-capture and Evidence-recording signals, the
installed process runs as the signed-in user in the interactive session rather
than as a traditional Session 0 service.

The installer defaults to a watchdog-managed Scheduled Task with no trigger and
no task-level restart policy. The external Watchdog owns persistent startup and
recovery, with Event Stream declared as a healthy startup prerequisite. An
explicit `-StartupMode Standalone` remains available for development without a
Watchdog.

## Process and security contract

The process listens only on one explicit IPv4 address. Loopback is the default;
an operator may explicitly select an RFC1918 private LAN address. Wildcard,
public, multicast, and inferred host bindings are rejected. The runtime does
not create a TLS endpoint, Windows Firewall rule, or public route. A private-LAN
HTTP deployment sends the Web bearer token and event contents without transport
encryption, so it is appropriate only on the operator's trusted LAN.

It holds two distinct regular-file credentials:

- the event-stream bearer token is used only by the backend loopback client;
- the Web bearer token protects every `/api/v1/*` request and is the only token
  supplied to browser JavaScript.

The embedded UI is same-origin, has no external resources, and receives a
restrictive Content Security Policy. It keeps the Web token in browser
`sessionStorage`, never a query parameter. Missing or rejected credentials are
handled by a non-blocking in-page password form; the UI never opens a native
`prompt`, `alert`, or `confirm` dialog that could block its main thread or CDP
control. The unauthenticated surface is only the embedded page and `/healthz`;
neither returns event contents or identifiers.

## HTTP interface

```text
GET /                                  embedded single-page UI
GET /healthz                           projection connection and cursor
GET /api/v1/osd                        exact current OSD projection
GET /api/v1/events?after=N&limit=N     durable replay
GET /api/v1/events/stream?after=N      live NDJSON
```

Replay additionally accepts the event journal's canonical `stream` selector.
Live filtering remains a browser presentation feature so unmatched global
records cannot create an invisible cursor gap.

The Streaming Log UI starts on the `action.runs` tab and can switch to the
`visual-log` tab. Both tabs project the same bounded in-memory event buffer;
the browser filters by `event.stream` after receiving the one unfiltered live
stream and an unfiltered durable tail replay. On initial load, the browser uses
the replay response's authoritative `lastSequence` to fetch at most the latest
100 envelopes before following from that cursor; it never replays the entire
journal merely to display a recent window. Selecting a tab therefore never
opens a second live stream or resets the global durable cursor. The text filter
is applied only to the selected tab.

Live ingestion retains at most 500 envelopes and coalesces DOM replacement to
at most once per 100 milliseconds instead of rebuilding the event list for
every envelope. A large network chunk also yields to the browser event loop at
least every 100 parsed envelopes. Pause freezes automatic rendering while the
single live reader continues to update the bounded buffer, and Resume renders
the current tab. Clear discards that buffer for both tabs but does not change
the durable cursor, so the next live envelope resumes without a gap.

Every browser event is an envelope containing the authoritative event and a
decimal-string `cursor`. Replay cursors are also decimal strings. JavaScript
therefore never has to round a `uint64` sequence through its Number type.

The OSD projection directly reuses `internal/actionosd.Model`, including strict
Action Sequence provenance and terminal expiry. It does not infer activity from
arbitrary domain payloads. Capture and recording indicators are read afresh
from their existing session-local Windows signals for each OSD request.
The Web OSD Mirror always renders Capture and Evidence as two independent
full-width rows. Active rows use cyan and yellow dots respectively; inactive
rows retain their positions with grey dots so Action and activity content does
not shift. This is a browser-mirror layout contract and does not change the
native Action OSD.

## Failure and reconnect behavior

Startup requires a healthy event stream and reconstructs only the same bounded
recent history used by the native OSD, subject to an optional explicit minimum
cursor. Invalid history is terminal.

After startup the projection reconnects only to the same configured event
source and resumes from its last processed durable cursor. A transport loss
sets `/healthz` to `503 degraded`; the UI may still be served but does not claim
the OSD projection is current. A schema or projection failure is terminal so
the process cannot repeatedly skip or conceal an invalid event.

Each browser stream independently asks the authoritative event client for
records after its supplied cursor. A disconnect closes that response; the UI
reconnects using the last envelope cursor. There is no cached-state, alternate
log, WebSocket, model, source, or protocol fallback.

## Live acceptance

The exact built artifact was installed through the maintained installer in the
signed-in interactive session. Acceptance verified PE GUI subsystem 2, no main
window, one explicit RFC1918 listener, matching installed hash, healthy Event
Stream and Capture Agent processes, browser bearer authentication, durable live
cursor advancement, raw event rendering, and the Action/Evidence OSD mirror.
The browser connected directly over the trusted LAN with no SSH tunnel and no
installer-created Windows Firewall rule.
