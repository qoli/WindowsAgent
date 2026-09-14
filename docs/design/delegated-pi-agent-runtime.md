# Delegated Pi agent runtime

**Status:** Partially landed

## Responsibility

`windows-agent-pi` is an independent delegated-agent runtime for general work
on a Windows machine. A supervising macOS agent submits a task and follows its
durable progress as it would a harness sub-agent. The runtime is not a Rule
Action, is not loaded into `windows-capture-agent.exe`, and does not turn Pi or
computer-use into WindowsAgent Core capabilities.

The process boundary is:

```text
macOS host agent
  -> authenticated SSH tunnel
  -> windows-agent-pi.exe (127.0.0.1:8791)
       -> PI WEB HTTP + per-session WebSocket (127.0.0.1:8504)
            -> pi-web-sessiond
                 -> Pi SDK / pi-agent-core / pi-ai
                 -> @injaneity/pi-computer-use
                 -> signed-in Windows interactive desktop

human browser/WebView
  -> PI WEB session view
```

PI WEB remains the session owner and detailed human UI. `windows-agent-pi`
owns only the stable upstream task contract, lifecycle normalization, and a
separate append-only journal. It never reads or rewrites PI WEB transcript
files.

## Runtime composition

The pinned Node environment is under `runtimes/windows-agent-pi/`:

- `@jmfederico/pi-web` `1.202609.0`;
- `@earendil-works/pi-ai`, `pi-agent-core`, and `pi-coding-agent` `0.85.1`;
- `@injaneity/pi-computer-use` `0.5.1`;
- `typebox` `1.3.7`.

`PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, `PI_WEB_DATA_DIR`,
`PI_WEB_CONFIG`, and `PI_COMPUTER_USE_WINDOWS_HELPER_PATH` select one explicit,
independent profile. The computer-use package must be installed into that Pi
profile. A missing daemon, package, helper, model, credential, permission, or
interactive desktop fails at its owning boundary. The runtime does not fall
back to the Pi CLI, another session daemon, another computer-use backend, or a
WindowsAgent Action.

## Host API

The delegated API listens on explicit loopback only and requires a bearer token
of at least 32 canonical bytes on every `/v1` route. The intended macOS path is
an authenticated SSH tunnel; port 8791 must not be exposed directly to an
untrusted network.

```text
POST /v1/delegated-tasks
GET  /v1/delegated-tasks/<taskId>
GET  /v1/delegated-tasks/<taskId>/events?after=<cursor>&limit=<n>
GET  /v1/delegated-tasks/<taskId>/events/stream?after=<cursor>
POST /v1/delegated-tasks/<taskId>/messages
POST /v1/delegated-tasks/<taskId>/cancel
```

Task creation accepts strict JSON:

```json
{
  "prompt": "Inspect the application and complete the requested work.",
  "cwd": "C:\\absolute\\workspace"
}
```

The manager creates a PI WEB session, establishes its per-session WebSocket,
then sends the initial prompt. This order prevents the first progress frames
from being lost. A successful response contains `taskId`, `state`, `sessionId`,
`piWebUrl`, and the latest durable sequence.

Messages require `{ "text": "...", "mode": "steer" }` or
`mode: "followUp"` and are accepted only while the task is running. A task in
`WAITING_INPUT` must be answered through the returned PI WEB session. This is
an intentional ownership boundary: the delegated API does not reproduce PI
`ask_user` or extension-dialog schemas and does not accept answers for them.
Cancellation first commits `CANCELLING`, then invokes PI WEB abort; the later
`agent.end` commits `CANCELLED`. Abort transport failure commits `FAILED`
rather than claiming cancellation.

## Durable lifecycle

The task states are `STARTING`, `RUNNING`, `WAITING_INPUT`, `CANCELLING`,
`COMPLETED`, `FAILED`, and `CANCELLED`. PI WEB events are retained inside
normalized `task.progress` records, including PI WEB's own per-session sequence
when present. `ask.opened` and `dialog.opened` move the task to
`WAITING_INPUT` and emit `task.attention.required` with only a stable reason and
the PI WEB URL. Their close events emit `task.attention.resolved`; the task
resumes `RUNNING` only after all outstanding attention has closed. Question,
option, title, answer, and dialog identity data remain private to PI WEB.
`session.error` is terminal failure. For ordinary completion, `agent.end`
starts terminal evaluation and the following PI WEB `status.update` must prove
that streaming, compaction, shell work, and the prompt queue are all idle. This
preserves queued `followUp` turns. During cancellation, `agent.end` commits
`CANCELLED`.

The journal uses the existing strict JSONL store but an independent data root
and the stream name `delegated.tasks`. Host cursors are journal sequences, not
PI WEB WebSocket sequences. Paging and NDJSON streaming therefore survive host
disconnects. If `windows-agent-pi` restarts with a non-terminal task in its
journal, recovery commits `FAILED` with `ABORTED_BY_RUNTIME_RESTART`; it does
not guess whether the old Pi session later finished.

## Evidence and ownership boundary

`COMPLETED` means PI ended the delegated run. It does not independently prove
that a visible Windows goal, file mutation, build, deployment, or external side
effect succeeded. The supervising host must use the task transcript, returned
artifacts, repository state, or a separately authorized WindowsAgent Evidence
or domain Action postcondition as appropriate.

PI WEB remains the user-facing transcript and interaction surface. The macOS
client's `web` command opens the returned session through the same SSH tunnel;
exact project/workspace route resolution and a packaged WebView shell are not
yet landed.

## Signed-in Windows acceptance

The 2026-09-15 baseline deployed Node 24.19.0, PI WEB 1.202609.0, Pi 0.85.1,
and `pi-computer-use` 0.5.1 into the dedicated current-user profile. The three
Scheduled Tasks ran in interactive session 1. A demo Pi task used only
`find_roots`, `observe_ui`, `expand_ui`, and `act_ui` against an existing
Calculator window, pressed `3`, `7`, multiply, `4`, `3`, and equals, then
observed and independently captured the visible result `1591`. Its durable
task state reached `COMPLETED`.

The first deployed lifecycle exposed visible Terminal windows and orphaned the
Node/helper children when Task Scheduler stopped the PowerShell wrappers. The
landed component owner replaces those wrappers with a GUI-subsystem process and
a kill-on-close Windows Job Object. Live stop validation observed zero owned
gateway, component, Node, or helper processes before restart; all three health
surfaces subsequently recovered and the completed task remained replayable.

This acceptance also bounded an upstream limitation instead of hiding it. With
no Calculator root present, `pi-computer-use` 0.5.1 rejected ref-less Windows
keypress even though the tool schema makes `ref` optional, while Explorer refs
became stale before the keypress. Calculator launch was therefore explicit
fixture setup through `windows-exec`; only the calculation and result
verification count as computer-use acceptance.

## Remaining work

The source-level API, PI WEB adapter, durable journal, shallow attention
projection, strict bearer boundary, version-pinned runtime bundle, native
per-user installer, macOS SSH-tunnel client, build integration, and contract
tests are landed. The following remain beyond the accepted Calculator baseline:

- cross-process desktop lease shared with mutable WindowsAgent Actions;
- project/workspace-resolved PI WEB deep links and packaged WebView;
- resolution or upstream repair of the Windows absent-application launch path;
- broader signed-in validation across long-running tasks, attention handling,
  and failure recovery beyond the accepted Calculator baseline.
