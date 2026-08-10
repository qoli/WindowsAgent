# Ephemeral Action Sequence

## Status

**Landed.** Rule schema version 6, model-facing strict JSON schema generation,
complete preflight, sequential finite and streaming child execution, durable
provenance events, Rule-scoped exclusion, and external cancellation are
implemented and tested.

## Purpose

`windows-ephemeral-action-sequence-v1` executes a small, disposable plan
without creating an Action package or persisting an executable definition. It
is a Host runtime, not a Starlark program and not a general workflow language.

The model submits one immutable JSON AST:

```json
{
  "ruleId": "EliteDangerous64.exe",
  "steps": [
    {"action": "elite-dangerous/ui-control", "inputs": {"control": "UI_Down"}},
    {"action": "elite-dangerous/ui-control", "inputs": {"control": "UI_Select"}}
  ]
}
```

The contract permits from one through twenty steps. Each step contains exactly
one existing Action ID and one literal JSON input object. Variables, output
references, branches, loops, nesting, mutation, and implicit defaults in the
model tool schema do not exist.

## Declaration and model schema

Every Rule schema version 6 descriptor explicitly declares the allowlist:

```json
{"ephemeralActionSequence":{"allowedActions":["game/action"]}}
```

An allowed Action must use a Core-owned runtime. A streaming child must be
linear and interruptible. Unknown, duplicated, unsupported, loop, or
non-interruptible streaming entries invalidate the Rule. An empty allowlist
disables sequence schema generation for that Rule.

`GET /v3/rules/{rule-id}/action-sequence-tool` loads every allowed Action's
package input schema and returns the strict function declaration
`run_action_sequence`. Its `steps` array has `minItems: 1`, `maxItems: 20`, and
one discriminated branch per allowed Action. This makes the accepted Action
IDs and their exact inputs visible to the model before generation.

## Execution

`POST /v1/action-sequences/invoke` validates the complete request, canonical
Rule identity, allowlist, child lifecycle, and every package input schema
before the first child can cause an effect. There is no partial preflight and
no alternate provider or compatibility path.

One sequence owns its Rule until terminal state. Another sequence, a normal
Action invocation, or an already active streaming Action for that Rule causes
an explicit conflict. Children execute strictly one at a time and the first
failure stops the sequence.

The sequence uses the ordinary invocation watch/status/stop surface and emits:

- `action.sequence.step.started`;
- `action.sequence.child.output` for a finite child;
- `action.sequence.child.event` wrapping each streaming child event with its
  step, Action ID, child invocation ID, and original event;
- `action.sequence.step.completed`.

The parent then emits exactly one ordinary terminal event. Stopping the parent
cancels the active streaming child and waits for its terminal event before the
parent becomes `CANCELLED`.

The submitted AST exists only in the live invocation while it runs and is
cleared at terminal state. Durable operational events and original child
outputs remain in `action.runs`; the executable sequence definition is never
written there.

## Deliberate limits

- no crash recovery or resume;
- no concurrent children;
- no data flow between steps;
- no condition, loop, retry, compensation, or nested sequence;
- no loop or non-interruptible streaming child;
- no persistence or later replay of the executable definition.
