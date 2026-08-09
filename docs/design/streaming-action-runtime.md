# Streaming Action Runtime

## Status

**Landed.** Rule schema version 5, unified invocation, durable callbacks,
natural linear completion, interruptible linear or loop execution, strict
Starlark orchestration, and the HTTP start/watch/status/stop surface are
implemented and tested. Elite Dangerous ships the first supervised linear
workflow, `elite-dangerous/leave-station`.

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
stream.activity(message=..., level=...)
task.sleep(milliseconds=...)
```

A child call must resolve inside the same owning Rule and must declare
`completion: return`. Event payloads and terminal output must pass the package
schemas. `stream.activity` emits a Host-validated, one-line display activity;
it does not bypass or replace the Action's domain event schema. Cancellation interrupts sleep and Starlark execution. Missing or
contradictory declarations, cross-Rule calls, streaming children, invalid
events, and invalid terminal output fail explicitly; no runtime or provider
fallback is attempted.

## Shipped supervised workflow

`elite-dangerous/leave-station` returns its watch URL immediately and emits
`AWAITING_AUTO_LAUNCH`. The higher model remains responsible for slow menu
arrangement through one-key `elite-dangerous/ui-control` calls. The workflow
then consumes finite `flight-status`, `ship-status`, and `ship-speed` evidence.
After Auto Launch is seen, the workflow requires a `MOVING` observation, five
samples without a classified Auto Launch prompt, Mass Lock ON, and two
classified `STOPPED` or `LOW_SPEED` observations before it may invoke
binding-resolved 100% throttle. `UNKNOWN` evidence never contributes to a Gate.
Mass Lock OFF then gates the 0% command. The workflow enters `VERIFYING_STOP`
instead of completing from input success: three consecutive frames must be
classified `STOPPED`, each backed by the dedicated slashed-zero pixel topology.
A qualified multi-digit OCR observation conflicts with that topology. The final
loop invokes only resident `ship-speed`. Flight prompt and Mass Lock fields use explicit
unobserved values in `SPEED_ONLY` events rather than repeating those pipelines
or retaining stale observations. Events distinguish
`commandedThrottle` from `observedSpeed*` and expose observation scope,
stop-gate age, and confirmations; missing or contradictory evidence and sample
limits fail explicitly, with no inferred state or alternate execution path.

## Deferred

- restart recovery for invocations interrupted by Agent process exit;
- retention or external indexing of the in-memory invocation status map;
- registration scheduler and Reaction dispatcher.
