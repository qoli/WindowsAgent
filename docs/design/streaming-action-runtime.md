# Streaming Action Runtime

## Status

**Landed.** Rule schema version 6, unified invocation, durable callbacks,
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
contradictory declarations, cross-Rule calls, invalid events, and invalid
terminal output fail explicitly; no runtime or provider fallback is attempted.

An interruptible linear Streaming Action may synchronously call another
interruptible linear Streaming Action in the same Rule. The runtime assigns a
child execution ID, wraps child start/events/completion/failure into the parent
stream, and propagates the parent context for cancellation. Composite Actions,
finite parents, loop children, non-interruptible children, cross-Rule calls,
and dependency cycles remain invalid. A child failure fails the parent; the
runtime does not silently replace it with another Action.

That child restriction belongs to the Starlark package runtime. The separate
Host-owned [Ephemeral Action Sequence](ephemeral-action-sequence.md) may invoke
a Rule-allowlisted linear, interruptible streaming Action and forwards its
events with child provenance; it does not widen `action.call`.

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

## Restart termination

At startup, the invocation manager replays the durable `action.runs` journal.
Any invocation with `action.started` but no terminal event is restored as a
queryable failed invocation and receives `action.failed` with
`errorCode=ABORTED_BY_AGENT_RESTART`. It is never resumed automatically because
the external game state and any previous input leases cannot be assumed valid.

Failure compensations may declare `critical=True` and an individual bounded
`timeout_milliseconds`. Critical compensations run first; each compensation
receives an independent timeout, so a blocked UI cleanup cannot consume the
throttle-zero or input-hold STOP budget.

## Deferred

- retention or external indexing of the in-memory invocation status map;
- registration scheduler and Reaction dispatcher.
