# Streaming Action Runtime

## Status

**Landed.** Rule schema version 5, unified invocation, durable callbacks,
natural linear completion, interruptible linear or loop execution, strict
Starlark orchestration, and the HTTP start/watch/status/stop surface are
implemented and tested. No shipped Game Rule declares a streaming Action yet.

## Model

`Action` remains the only executable capability abstraction. Its required
`execution` declaration selects the completion channel:

```json
{"completion":"return"}
```

or:

```json
{"completion":"stream","lifecycle":"linear","interruptible":true}
```

`return` waits for a schema-validated terminal output and writes no callback
event. `stream` first commits `action.started`, then immediately returns an
invocation identity and NDJSON watch URL. A streaming Action declares either:

- `linear`: it naturally reaches `action.completed` or `action.failed`;
- `loop`: it runs until cancellation or failure. A natural return is invalid
  and becomes `action.failed`.

`interruptible` is explicit and independent of lifecycle. When true, the
start response also includes a stop endpoint. Repeated stop requests for the
same invocation are idempotent. There is no completed-but-awaiting-stop state.

## Invocation surface

```text
POST /v1/actions/invoke
GET  /v1/action-invocations/{invocation-id}
GET  /v1/action-invocations/{invocation-id}/events?after={cursor}
POST /v1/action-invocations/{invocation-id}/stop
```

Finite invocation returns HTTP `200` with `COMPLETED` and `output`. Streaming
invocation returns HTTP `202` with `RUNNING`, `watch`, and an optional `stop`.
The callback response is `application/x-ndjson`; it replays from the returned
cursor, follows new correlated events, and closes after
`action.completed`, `action.failed`, or `action.cancelled`.

Every streaming invocation uses one `act_<hex>` identity as its session and
correlation ID. Events are serialized into one causation chain in the durable
`action.runs` stream. Terminal events belong to the host, not plugin code.

## Streaming package

`windows-streaming-action-v1` packages declare exact files, input/output/event
schemas, and explicit step/output/event/sleep limits. The Starlark entrypoint
has only three orchestration primitives:

```text
action.call(id=..., inputs=...)
stream.emit(type=..., payload=...)
task.sleep(milliseconds=...)
```

A child call must resolve inside the same owning Rule and must declare
`completion: return`. Event payloads and terminal output must pass the package
schemas. Cancellation interrupts sleep and Starlark execution. Missing or
contradictory declarations, cross-Rule calls, streaming children, invalid
events, and invalid terminal output fail explicitly; no runtime or provider
fallback is attempted.

## Deferred

- shipping the first executable-specific streaming workflow;
- restart recovery for invocations interrupted by Agent process exit;
- retention or external indexing of the in-memory invocation status map;
- registration scheduler and Reaction dispatcher.
