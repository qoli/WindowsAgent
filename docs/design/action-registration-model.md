# Action Registration Model

## Status

**Partially landed.** Rule schema version 5, strict runtime-profile and Action
declarations, explicit execution semantics, Monitor and Reaction
registrations, read-only catalogs, and direct invocation are implemented.
Registration execution is deferred.

## Model

An `Action` is the only executable capability definition. It owns a package
path and runtime and may declare that it is eligible for one or both
registration forms:

```json
{
  "schemaVersion": 5,
  "description": "Read the live Rule before acting.",
  "runtimeProfiles": {},
  "actions": {
    "game/status": {
      "path": "Actions/status",
      "runtime": "windows-observation-v1",
      "execution": {"completion": "return"},
      "registrableAs": ["monitor", "reaction"]
    }
  },
  "registrations": {}
}
```

`registrableAs` is permission to register, not a registration. An empty
`registrations` object means the Action remains directly callable only.
For example, the Elite Dangerous compass, flight-status, and ship-status
Actions are eligible for both registration types while their empty
registration catalog leaves them strictly on-demand. Flight-status is a pure
postprocessor for the complete raw flight-prompt-text result; eligibility does
not implicitly connect or schedule those two Actions. The ship-status Action's `UNKNOWN` results preserve
insufficient visual evidence rather than silently converting it to `OFF`; its
three-row relative-geometry check reports Mass Lock, Landing Gear, and Cargo
Scoop independently.

`execution` is required independently from registration. It declares whether
direct invocation completes through the HTTP return value or a durable event
stream. Streaming Actions additionally declare `linear` or `loop` lifecycle
and an explicit interruption capability. See
[Streaming Action Runtime](streaming-action-runtime.md).

A Monitor registration adds a timer and an emitted event contract:

```json
{
  "type": "monitor",
  "action": "game/status",
  "input": {},
  "monitor": {
    "intervalMs": 2000,
    "emit": {"stream": "game.state", "eventType": "game.status"}
  }
}
```

A Reaction registration adds an event selector and input template:

```json
{
  "type": "reaction",
  "action": "game/open-map",
  "input": {},
  "reaction": {
    "stream": "game.state",
    "eventType": "game.ready",
    "match": {"payload.state": "^ready$"}
  }
}
```

The registration ID is its key in `registrations`. It is lifecycle identity;
the Action ID remains executable capability identity.

## Invariants

- every package lives below `Actions/`;
- every Action explicitly declares `registrableAs`, including an empty list;
- every Action explicitly declares one valid execution contract;
- a registration must reference an existing Action;
- its type must appear in that Action's `registrableAs` declaration;
- Monitor intervals are positive and emitted stream/event names are canonical;
- Reaction regular expressions compile before the Rule is accepted;
- unknown types, missing triggers, malformed input, and path escapes fail;
- no registration is synthesized and no alternate Action is selected.

The live catalogs are:

```text
GET /v4/rules/{canonical-rule-id}/runtimes
GET /v3/rules/{canonical-rule-id}/actions
GET /v3/rules/{canonical-rule-id}/registrations
```

A runtime profile is lifecycle configuration, not executable capability and
not a registration. For example, an OCR worker may use
`residency: while-rule-active` so model initialization follows foreground Rule
activation. An Action must still reference that profile explicitly, and no
timer, event, or Action invocation is synthesized from residency.

The v1 Script catalog remains the existing finite projection of Actions using the
`windows-observation-v1` runtime. It does not change Action or registration
semantics.

## Deferred

- Monitor scheduler and lifecycle control;
- Reaction event subscription and dispatch;
- registration enable/disable overrides owned by a Game session;
- restart recovery for running streaming invocations.
