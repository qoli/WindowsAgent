# Game Module Registry v2

## Status

**Partially landed.**

Strict Rule schema version 2 and the read-only Modules catalog are implemented.
The Crimson Desert inventory package is registered as a `query` module. The
Palworld Rule registers `screenparser-onnx-dml-v1` as an on-demand
`preprocessor`; it has no Scheduled Task, capture loop, or event producer. No
model reactor or action module is currently shipped.

## Model

Every `Rules/<Executable.exe>/rule.json` owns one `modules` registry. Each
module declaration has independent semantic kind, runtime, and package path:

```json
{
  "schemaVersion": 2,
  "description": "Read the live Rule before acting.",
  "modules": {
    "game/status": {
      "kind": "loop",
      "runtime": "windows-script-v2",
      "path": "Modules/status"
    }
  }
}
```

Supported kinds are:

- `query`: finite read-only result;
- `preprocessor`: finite transformation of caller-supplied evidence;
- `loop`: independent process that owns its observation/event loop;
- `reactor`: event subscriber that may request declared actions;
- `action`: finite Windows operation.

`query`, `preprocessor`, and `loop` packages live below `Modules/`, reactors below `Reactors/`,
and actions below `Actions/`. Missing directories, path escapes, duplicate
cross-Rule module IDs, unsupported kinds, and malformed descriptors fail
explicitly.

`GET /v2/rules/{canonical-rule-id}/modules` exposes the live classified
registry. The v1 Scripts catalog projects only `query` modules while the
existing finite observation launcher is being migrated; it never selects a
different module when a query is missing or invalid.

## Deferred

- shared control-plane validation for every per-kind manifest schema;
- Game session/config revision binding;
- automatic cross-Rule module lifecycle and process isolation;
- event-stream credentials and stream declarations;
- migration of the v1 query runtime to `windows-script-v2`.
