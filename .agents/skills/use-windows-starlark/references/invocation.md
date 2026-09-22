# Preflight, invoke, and follow

## Experimental client prerequisite

Use native `windows-starlark-check` and `windows-starlark-invoke` clients built
for the Harness host (on Windows, `.exe`). These are local console clients, not
installed Agent payloads, and this Skill does not bundle them. Record their
source revision when supplied by the experiment. Do not assume the stable
Skill bundle or Windows installer placed them on PATH.

If no clients are available, the current reproducible option is to obtain the
public `qoli/WindowsAgent` source at the revision selected for the experiment,
with a Go toolchain satisfying its `go.mod`, then build locally:

```bash
go build -o /absolute/private/tools/windows-starlark-check ./cmd/windows-starlark-check
go build -o /absolute/private/tools/windows-starlark-invoke ./cmd/windows-starlark-invoke
```

Run those build commands from that source checkout and use absolute output
paths on subsequent calls. This is a client preparation dependency, not a
requirement to install developer Skills, build/deploy the Windows runtime, or
change ordinary PC onboarding. If the dependency cannot be met, report it.

Use the installed `use-windows-pc` Skill's actual location to load its
`scripts/resolve-pc.sh` (Codex's default example below). It derives
`WINDOWS_AGENT_HTTP_ORIGIN` from the private PC configuration without printing
it. Other Harnesses should use their installed Skill path.

```bash
pc_skill_root="${CODEX_HOME:-$HOME/.codex}/skills/use-windows-pc"
source "$pc_skill_root/scripts/resolve-pc.sh"
curl --fail-with-body --silent --show-error "$WINDOWS_AGENT_HTTP_ORIGIN/healthz"
```

Require `status: ok`. For a desktop-dependent task also obtain current capture
and foreground through the ordinary PC workflow. A filesystem-only task does
not require a screenshot. Health alone does not establish Starlark support;
preserve an unsupported-endpoint response rather than attempting deployment.
Keep this unauthenticated Agent endpoint on the user's trusted LAN/private
network; this workflow does not change firewall or listener configuration.

## Preflight and submit

Copy the asset's five files into a private package directory. For its read-only
example, create a separate inputs file containing an existing, authorized,
small UTF-8 Windows text file path:

```json
{"path":"C:\\Users\\Example\\Documents\\sample.txt"}
```

`Example` is a placeholder, not target discovery. This example reads an existing
file without mutation; do not manufacture a remote file just to make it pass.

```bash
windows-starlark-check --package /absolute/private/package --json
windows-starlark-invoke \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --package /absolute/private/package \
  --inputs /absolute/private/inputs.json > /absolute/private/accepted.json
```

Check each command's exit status before continuing. The checker accepts a
directory or `.zip`, prints `valid`, `runtime`, `title`, `version`, `digest`
on success, and does **not** accept inputs. It compiles package/schema/Starlark
structure without executing host calls or checking their dynamic signatures.
The invoke client accepts a directory, validates the strict inputs object
against the schema, builds the canonical ZIP, and submits it. It has no
preflight-only inputs flag or wait-for-completion mode.

WindowsAgent revalidates the archive and inputs. The wire request is
`POST /v1/starlark-actions/invoke` with JSON:

```json
{"schemaVersion":1,"packageBase64":"<canonical ZIP base64>","inputs":{}}
```

The client handles ZIP ordering and encoding; an ordinary ZIP containing a
wrapper folder is not equivalent. Keep the checker digest and accepted JSON.
A local failure is not a Windows invocation. A transport failure after submission
may have accepted work: preserve the response/request evidence and inspect known
invocation state before deciding whether another submission is safe.

## Follow the terminal lifecycle

HTTP 202/CLI exit 0 means accepted, not completed. The response supplies
`invocationId`, `actionId` (`windows/ephemeral-starlark`), `runtime`, `state`,
`execution`, `watch` and `stop`. Watch and stop URLs are relative to the same
configured Agent origin, not another listener. Retain the invocation ID.

- GET the returned `watch.url`: `application/x-ndjson`, one JSON event per line,
  **not SSE**. It already includes `after=<cursor>`; do not append a duplicate.
- Save each consumed event's `sequence`. This is the durable resume cursor,
  not `eventId`, a line count, or a contiguous per-invocation counter.
- On interruption resume
  `GET /v1/action-invocations/{invocationId}/events?after={lastSequence}`.
  If no event has been consumed use the initial `watch.afterCursor`.
- Status is `GET /v1/action-invocations/{invocationId}`. Running states are
  `RUNNING` and `CANCELLING`; terminal states are `COMPLETED`, `FAILED`,
  `CANCELLED`. Status exposes `output` or `error`, `errorCode`, `errorStage`.
- Exactly one normal terminal event is `action.completed`, `action.failed`,
  or `action.cancelled`; inspect its payload and final status. EOF or HTTP 200
  alone is not a terminal result. If status is terminal and the terminal event
  is already consumed, do not reopen a stream after that cursor expecting it
  to replay again.
- To cancel, POST the returned `stop.url` with no body, then continue following
  status/events until terminal. A 202 stop response is only a cancellation
  request; it can also return an already-terminal invocation. Stopping the
  local client or curl does not cancel Windows work.

Example command forms (replace `INVOCATION_ID` and `LAST_SEQUENCE` with the
saved values):

```bash
curl --fail-with-body --silent --show-error --no-buffer \
  "$WINDOWS_AGENT_HTTP_ORIGIN/v1/action-invocations/INVOCATION_ID/events?after=LAST_SEQUENCE"
curl --fail-with-body --silent --show-error \
  "$WINDOWS_AGENT_HTTP_ORIGIN/v1/action-invocations/INVOCATION_ID"
curl --fail-with-body --silent --show-error --request POST \
  "$WINDOWS_AGENT_HTTP_ORIGIN/v1/action-invocations/INVOCATION_ID/stop"
```

`action.started` records package provenance; `action.activity` carries script
progress. A completed schema-valid result can still fall short of the user's
application goal. Check the declared postcondition and report separately:
local preflight, acceptance, runtime terminal state, observed effect, cleanup,
and any remaining uncertainty. Preserve typed failures rather than switching
execution providers or treating cancellation as successful cleanup.
