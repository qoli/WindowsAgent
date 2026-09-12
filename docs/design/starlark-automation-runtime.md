# Starlark Automation Runtime

## Status

**Partially landed as `windows-starlark-action-v1`.** Package loading, local
preflight, deterministic upload, asynchronous invocation, cancellation,
durable events, process execution, and filesystem operations are implemented.
Signed-in interactive Windows acceptance remains required before this design
can be classified as Landed.

## Execution model

Starlark is the only caller-authored language for this runtime. A caller builds
one package containing `manifest.json`, `main.star`, `TASK.md`, and strict input
and output JSON Schemas. `windows-starlark-check` parses that package locally;
`windows-starlark-invoke` validates it again, creates its deterministic ZIP,
reads a strict JSON inputs object, and posts both to WindowsAgent.

WindowsAgent validates the archive and inputs again, creates one durable Action
invocation, and runs `main.star` inside the WindowsAgent Go process. The code
therefore inherits the WindowsAgent user token, environment, and signed-in
interactive desktop session. It does not run inside the non-interactive SSH
session and does not implicitly route through PowerShell, `cmd.exe`, Windows
MCP, SSH, or any fallback transport. A package may explicitly select an
executable through the structured `windows.process.run` interface.

The installer's `-AgentRunLevel Limited|Highest` choice determines the runtime
privilege. A package cannot select or silently elevate its own token. The
default remains `Limited`.

## Package and host surface

The manifest declares positive wall-time, Starlark-step, result-size, and
per-stream process-output limits. `main.star` must define `main(ctx)`, receives
the schema-validated `ctx.inputs` object, and returns one output matching the
declared output schema. `load()` is not supported.

The current host surface is:

- `windows.process.run(executable=, argv=, cwd=, env=, stdin=)` starts one
  process using an argv array and returns its PID, exit code, stdout, stderr,
  and duration. A non-zero exit code is data; launch, wait, cancellation, and
  output-capture failures are runtime errors.
- `windows.fs.read`, `write`, `stat`, `list`, `mkdir`, `copy`, `move`, and
  `remove` operate on the Windows filesystem. Filesystem failures are explicit.
- `task.activity(message=, level=)` commits an `info`, `warning`, or `error`
  activity to the durable invocation journal.
- `task.elapsed_milliseconds()` reports elapsed runtime time.
- `task.fail(code=, message=)` terminates the Action with one caller-owned,
  typed failure code and stage.

There is deliberately no command-string shell API and no PowerShell fallback.
The package owns operation ordering and its real postcondition; a successful
file write or process launch does not prove an application or UI goal.

## HTTP and lifecycle

`POST /v1/starlark-actions/invoke` accepts:

```json
{
  "schemaVersion": 1,
  "packageBase64": "<deterministic ZIP as base64>",
  "inputs": {}
}
```

It returns HTTP `202` with an invocation ID plus `watch` and `stop` targets.
The common Action lifecycle endpoints remain authoritative:

```text
GET  /v1/action-invocations/{invocation-id}
GET  /v1/action-invocations/{invocation-id}/events?after={cursor}
POST /v1/action-invocations/{invocation-id}/stop
```

The event stream ends in exactly one `action.completed`, `action.failed`, or
`action.cancelled` event. Remote runtime failures retain their typed error code
and stage. The invoke CLI prints the accepted remote JSON unchanged; it does
not reinterpret a response as completed runtime work.

`windows.process.run` is a synchronous child-process call in this version. It
does not promise detached-process ownership or whole descendant-tree cleanup.
Those semantics need a separate explicit host contract before they are added;
the runtime does not emulate them with a shell or background-command fallback.

## Local preflight boundary

Local preflight proves only package structure, manifest and schema validity,
the Starlark dialect, presence of `main(ctx)`, and inputs-schema compatibility.
It cannot prove Windows paths, installed programs, process results, privileges,
desktop visibility, UI state, or the requested postcondition. Those answers
come only from the Windows invocation and its durable events.

Example:

```json
{
  "message": "hello from Windows"
}
```

Save that object as an inputs file, then run:

```bash
go run ./cmd/windows-starlark-check \
  --package docs/examples/windows-starlark-hello

go run ./cmd/windows-starlark-invoke \
  --url http://Windows-PC:8787 \
  --package docs/examples/windows-starlark-hello \
  --inputs /absolute/path/to/inputs.json
```

## Deployment boundary

The checker and invoke commands are local console clients; they are not copied
into the installed Agent binary set and are not Watchdog targets. The runtime
itself is hosted by `windows-capture-agent.exe`, so normal Agent installation
or transactional deployment publishes it.

The default Agent listener is unauthenticated and binds `0.0.0.0:8787`. This
endpoint can now execute mutable, general Windows operations with the Agent's
installed run level. It must remain reachable only through a trusted LAN or
private overlay network; do not expose it directly to the public Internet.

## Remaining acceptance

Live acceptance must verify the installed Agent hash and run level, a non-zero
interactive Session ID, fresh capture and foreground health, a package upload,
argv-preserving process execution, a filesystem mutation and read-back,
cancellation, durable terminal replay, and an independently observed desktop
postcondition. Local tests and a Windows cross-build are not substitutes for
that signed-in-session evidence.
