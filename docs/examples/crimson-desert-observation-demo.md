# Crimson Desert Scripted Observation Demo

## Status

**Superseded by the fixture-verified inventory package.**

The child runtime, package runner, and observer core now exist. Use
[Crimson Desert Inventory Script](crimson-desert-inventory-job.md) for the
implemented example and its explicit live limitation.

This demo shows why one unified observer is useful. A task may combine current
memory state with an explicitly selected save-file snapshot while still
producing one JSON result.

## Package

```text
crimson-desert/state-summary/
  manifest.json
  TASK.md
  main.star
  output.schema.json
```

The manifest grants only:

- memory `modules`, `scan`, and `readBatch` for the exact current
  `CrimsonDesert.exe` process;
- file `stat`, `read`, and `hash` within the registered save root;
- bounded calls, bytes, steps, result size, and wall time.

## Script Shape

```python
def main(ctx):
    modules = observer.memory.modules()
    player = locate_and_read_player(modules)

    selected_save = job.input("save")
    save_stat = observer.file.stat(selected_save)
    save_hash = observer.file.hash(selected_save, "sha256")

    return {
        "schemaVersion": 1,
        "live": {
            "position": player.position,
            "observedAt": player.observed_at,
        },
        "save": {
            "pathId": selected_save,
            "size": save_stat.size,
            "sha256": save_hash,
            "modifiedAt": save_stat.modified_at,
        },
    }
```

Helper functions above are illustrative local functions in `main.star`, not
dynamic modules.

The script does not ask the observer to choose the “latest” save. The job input
must already identify an authorized logical file. There is no directory watch
or recurring memory sampling.

## Execution

WindowsAgent launches one script runner and one `windows-observer.exe`.
Memory and file calls use the same `observer/call` protocol session and share
one deadline and accounting ledger.

If the memory signature is ambiguous, the file changes identity during the
read, any budget is exceeded, or the final object violates the output schema,
the complete job fails. One source is not silently substituted for the other.

## Result

```json
{
  "schemaVersion": 1,
  "live": {
    "position": {"x": 123.5, "y": 45.25, "z": -8.0},
    "observedAt": "2026-07-27T12:00:00.000Z"
  },
  "save": {
    "pathId": "slot0-primary",
    "size": 1048576,
    "sha256": "...",
    "modifiedAt": "2026-07-27T11:52:00.000Z"
  }
}
```

This is a demonstration of orchestration and contract shape. It is not a claim
that the current Crimson Desert player-position chain is validated.
