# Crimson Desert Inventory Script

## Status

**Memory and save fallback are fixture-verified; live capability unsupported.**

The runnable package is at
`ObservationScripts/CrimsonDesert/inventory`. Its Starlark execution and output
schema pass the deterministic integration fixture.

On 2026-07-27, a fresh live read against `CrimsonDesert.exe` `1.0.0.2145`
(`d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f`)
found the unique manager signature. With the inventory UI open, both reviewed
static chains still became unreadable before reaching a valid `0xC8` item
container. The Cheat Engine evidence obtains that container from a transient
runtime register capture. Hooks, injection, debugger control, thread
suspension, and memory writes are outside the read-only observer boundary.

The package therefore has one explicit fallback: if the memory attempt has a
typed source failure, it decodes one caller-selected authorized `save.save`.
The decoder interface and deterministic fixture are implemented. The pinned
`crimson-rs/inventory@bb730180` decoder binary is not yet packaged or
registered, so the capability still fails closed in a live run.

This package answers one question:

> What inventory entries can be read from current process memory, or otherwise
> from one explicitly selected save snapshot?

It does not run continuously, watch the process or filesystem, choose a
“latest” save, use OCR, or infer localized item names.

## Package Layout

```text
crimson-desert/inventory/
  manifest.json
  TASK.md
  main.star
  output.schema.json
```

## `manifest.json`

Implemented shape (digests are pinned in the package):

```json
{
  "schemaVersion": 1,
  "id": "crimson-desert/inventory",
  "version": 1,
  "title": "Read the current character inventory",
  "entrypoint": "main.star",
  "taskDocument": "TASK.md",
  "outputSchema": "output.schema.json",
  "files": {
    "main.star": {"sha256": "<sha256>"},
    "TASK.md": {"sha256": "<sha256>"},
    "output.schema.json": {"sha256": "<sha256>"}
  },
  "permissions": {
    "memory": {
      "target": "crimson-desert/current-process",
      "operations": ["modules", "scan", "resolveRip", "readBatch", "readStrided"],
      "maxCalls": 12,
      "maxBytesRead": 536870912
    },
    "file": {
      "roots": ["crimson-desert-saves"],
      "operations": ["decode"],
      "maxCalls": 1,
      "maxBytesRead": 67108864
    }
  },
  "limits": {
    "wallTimeMs": 5000,
    "maxSteps": 500000,
    "maxResultBytes": 262144,
    "maxLogBytes": 32768
  }
}
```

The file permission grants only one decode call against the named save root.
It grants no enumeration, arbitrary read, watch, or implicit file selection.

## `TASK.md`

The pinned task document records:

- purpose: return inventory slot records from memory, then selected save;
- precondition: exact supported executable build and any required menu/state;
- API use: module identity, bounded signature scan, then batched reads;
- output semantics: stable slot index, raw item identifier, quantity, and
  explicit slot state;
- exclusions: no guessed names, descriptions, icons, rarity, value, or claim
  that a save snapshot equals current live state;
- failures: unsupported build, signature not found, ambiguous match, invariant
  failure, torn/short read, budget exhaustion, and schema failure;
- evidence: the build(s) and runtime transitions used to validate the layout.

## `main.star`

Implemented flow (abridged; the package file is authoritative):

```python
def read_from_save():
    return observer.file.decode(
        path = job.input(name = "save"),
        decoder = "crimson-rs/inventory@bb730180",
        options = {"scope": "active-character-inventory"},
    )

def main(ctx):
    memory = job.attempt(
        source = "process-memory",
        function = read_from_memory,
    )
    if memory["ok"]:
        return memory_result(memory)

    save = job.attempt(source = "save-file", function = read_from_save)
    if save["ok"]:
        return save_result(memory, save)

    job.fail(
        code = "INVENTORY_ALL_SOURCES_FAILED",
        message = memory["error"]["code"] + ", " + save["error"]["code"],
    )
```

All helper functions and build-specific constants are reviewed content inside
the one pinned `main.star`. They are not observer profiles, remote modules, or
runtime downloads.

## `output.schema.json`

The implemented schema pins the source-dependent provenance, exact attempt
order, bounded counts, and normalized raw item fields. A memory success has
one succeeded attempt. A save success must show the failed memory attempt
followed by the succeeded save attempt.

If an empty slot cannot be proven distinct from an unreadable slot, the schema
must not pretend otherwise. The contract should be revised before registration
rather than encode uncertainty as a false zero.

## Job Description

The application-level job is small because the package owns the task:

```go
ScriptObservationJobSpec{
    Version:  1,
    JobID:    jobID,
    Deadline: deadlineUTC,
    Capability: CapabilityIdentity{
        ID:      "crimson-desert/inventory.read",
        Version: 1,
        SHA256:  capabilitySHA256,
    },
    ScriptPackage: ScriptPackageIdentity{
        ID:             "crimson-desert/inventory",
        Version:        1,
        ManifestSHA256: manifestSHA256,
        PackageSHA256:  packageSHA256,
    },
    Inputs: map[string]json.RawMessage{
        "save": selectedAuthorizedSavePath,
    },
}
```

Addresses, signatures, pointer chains, and output construction no longer
appear in the Job. They are pinned and reviewed together in the script package.

## Expected Result

```json
{
  "schemaVersion": 1,
  "source": {
    "kind": "save-file",
    "processImageSha256": null,
    "saveModifiedAt": "2026-07-27T13:42:00Z",
    "decoder": "crimson-rs/inventory@bb730180"
  },
  "attempts": [
    {
      "source": "process-memory",
      "status": "failed",
      "errorCode": "INVENTORY_SIGNATURE_NOT_FOUND"
    },
    {"source": "save-file", "status": "succeeded", "errorCode": null}
  ],
  "inventory": {
    "recordCount": 2,
    "occupiedCount": 2,
    "items": [
      {
        "slot": 4,
        "itemId": 11,
        "quantity": 77789,
        "pairedItemId": null,
        "inventoryKey": 1,
        "instanceId": 9001
      },
      {
        "slot": 5,
        "itemId": 50001,
        "quantity": 683,
        "pairedItemId": null,
        "inventoryKey": 1,
        "instanceId": 9002
      }
    ]
  }
}
```

## Acceptance Criteria

This package cannot become a registered live capability until:

- the exact game build is digest-identified;
- inventory root and row layout survive repeated attach/read verification;
- empty, occupied, moved, added, removed, and quantity-changed slots are
  distinguished with controlled runtime transitions;
- any required memory-attempt menu state is explicit;
- ambiguous signatures fail;
- observer byte/call budgets are sufficient and fixed;
- the returned JSON passes the pinned schema;
- the exact save decoder is packaged, registered, and bound by identity;
- the save timestamp is reported as snapshot freshness;
- both source failures produce `INVENTORY_ALL_SOURCES_FAILED`;
- no undeclared source or inferred semantic field is used.
