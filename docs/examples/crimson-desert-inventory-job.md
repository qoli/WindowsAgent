# Crimson Desert Inventory Script

## Status

**Implemented and live-verified.**

The package lives at
`Rules/CrimsonDesert.exe/Actions/inventory`. It owns the
game-specific process layout, save-decoder DLL, ABI declarations, record
layout, return codes, backpack filter, output conversion, and source order.
The generic Go launcher knows none of those details. It derives
`CrimsonDesert.exe` from the owning Rule and validates the request against the
package's `input.schema.json`.

## Finite source order

1. Read the reviewed process-memory chain once.
2. If that source cannot produce a valid result, list the package-declared
   Crimson Desert LocalAppData root once.
3. Require one account directory and select the uniquely newest regular
   `<slot>/save.save`.
4. Resolve the resulting same-job blob.
5. Load manifest alias `save-decoder` in the Script Runner.
6. Bind and call the three pinned crimson-rs exports from Starlark.
7. If both application sources fail, return
   `INVENTORY_ALL_SOURCES_FAILED`.

There is no caller-supplied path, polling, file watch, DLL/version fallback,
decoder fallback, hidden retry, or alternate source.

## Launcher request

```json
{
  "inputs": {}
}
```

The package declares its logical root through the portable LocalAppData
resolver. The Host resolves it locally; the caller never supplies or sees an
absolute root.

## Native declaration

```json
{
  "nativeLibraries": {
    "save-decoder": {
      "platform": "windows-amd64",
      "artifact": "native/windows-amd64/crimson-rs.inventory.bb730180.dll",
      "maxCalls": 4,
      "maxNativeMemoryBytes": 131072
    }
  }
}
```

The artifact is also in `manifest.files`. WindowsAgent Core knows only alias,
package-relative artifact, platform, and limits.

## Package-owned ABI

`main.star` defines:

- `crimson_save_load_from_file`;
- `crimson_save_list_inventory_items`;
- `crimson_save_free`;
- signed return codes `0` and `-11`;
- the eight-`u32` plus two-`u64` record, naturally aligned to 48 bytes;
- backpack `inventoryKey = 2`;
- record filtering and output-schema conversion.

The first call loads the job blob and returns a handle. The second export is
called once with a null record pointer to obtain count, then once with
`native.out(native.array(record_type, count))`. The fourth and final native
call frees the save handle. The package rejects more than 2,048 records,
budgets 128 KiB of cumulative native call buffers, and allows at most 768 KiB
for either a decoded native call result or the final job output. Both result
limits stay below the 1 MiB framed-protocol boundary.

## Verification boundary

Tests prove memory-first behavior, package-owned save discovery and selection,
two-stage record query, 48-byte/8-byte-aligned layout, generic FFI scalar/out handling,
declared-artifact loading, alias-only load, missing export failure, native
limits, forged blob rejection, and schema-valid output.

The terminal job output contains the schema-declared occupied item records.
Privacy-minimized live validation reports contain only attempts, selected
source kind, record count, and bounded provenance; they do not reproduce save
paths, item details, memory contents, or private data.
