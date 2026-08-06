# Action Registration Model

## Status

**Partially landed.** Rule schema version 3, strict Action declarations,
explicit Monitor and Reaction registrations, and read-only HTTP catalogs are
implemented. Registration execution is deferred.

## Model

An `Action` is the only executable capability definition. It owns a package
path and runtime and may declare that it is eligible for one or both
registration forms:

```json
{
  "schemaVersion": 3,
  "description": "Read the live Rule before acting.",
  "actions": {
    "game/status": {
      "path": "Actions/status",
      "runtime": "windows-observation-v1",
      "registrableAs": ["monitor", "reaction"]
    }
  },
  "registrations": {}
}
```

`registrableAs` is permission to register, not a registration. An empty
`registrations` object means the Action remains directly callable only.

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
- a registration must reference an existing Action;
- its type must appear in that Action's `registrableAs` declaration;
- Monitor intervals are positive and emitted stream/event names are canonical;
- Reaction regular expressions compile before the Rule is accepted;
- unknown types, missing triggers, malformed input, and path escapes fail;
- no registration is synthesized and no alternate Action is selected.

The live catalogs are:

```text
GET /v3/rules/{canonical-rule-id}/actions
GET /v3/rules/{canonical-rule-id}/registrations
```

The v1 Script catalog remains a compatibility projection of Actions using the
`windows-observation-v1` runtime. It does not change Action or registration
semantics.

## Deferred

- Monitor scheduler and lifecycle control;
- Reaction event subscription and dispatch;
- generic direct Action invocation beyond the existing observation launcher;
- registration enable/disable overrides owned by a Game session;
- durable execution lifecycle events for mutating Actions.
