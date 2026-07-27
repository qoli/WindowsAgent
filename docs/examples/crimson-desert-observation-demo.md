# Crimson Desert Scripted Observation Demo

## Status

**Superseded by the live-verified inventory package.**

The child runtime, package runner, and observer core now exist. Use
[Crimson Desert Inventory Script](crimson-desert-inventory-job.md) for the
implemented example and its explicit live limitation.

This retired sketch shows why one unified observer is useful. It is not a
current launch contract. The implemented inventory package now owns bounded
save discovery and selection instead of accepting a caller-selected path.

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

    listing = observer.file.list(
        path = {"root": "declared-save-root", "relative": "."},
        maxDepth = 3,
        maxEntries = 4096,
    )
    selected_save = select_save(listing)
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

This obsolete sketch used a caller-selected logical file. The implemented
inventory package instead uses permission-gated bounded listing and a
deterministic newest-save policy in Starlark. There is still no directory watch
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
