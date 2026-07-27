# Crimson Desert Inventory Read

## Purpose

Return raw occupied backpack records from the first valid source in this exact
order:

1. the explicitly bound current `CrimsonDesert.exe` process;
2. one user-selected save file.

This is one finite job. It does not poll, watch files, select the newest save,
retry invisibly, or substitute a decoder/version/source.

## Preconditions

- The process identity and executable SHA-256 are resolved by the trusted Host.
- `inputs` passes `input.schema.json`.
- The `crimson-desert-saves` grant is bound by the Host to one authorized
  absolute root, while the package receives only its alias and the selected
  root-relative file.
- The package-native artifact is
  `native/windows-amd64/crimson-rs.inventory.bb730180.dll`.
- Its manifest alias is `save-decoder`.

## Memory attempt

`main.star` validates the reviewed executable hash, locates one manager
signature, resolves the fixed pointer chain, reads the bounded array header,
and performs one strided record read. Missing, ambiguous, or invalid
application data makes the memory source attempt fail.

## Save attempt

Only after memory fails, `observer.file.open_blob` copies the explicitly
selected save into the current job blob and accounts the bytes.
`native.blob_path` resolves that same-job reference. The trusted Starlark then:

1. loads `native.load_library("save-decoder")`;
2. binds `crimson_save_load_from_file`;
3. binds `crimson_save_list_inventory_items`;
4. binds `crimson_save_free`;
5. declares the eight-`u32` plus two-`u64` 48-byte record;
6. rejects a count above the shared 2,048-record task bound;
7. reads one fixed record array, filters `inventoryKey = 2`, converts to JSON,
   and frees the save handle.

All Crimson exports, return codes, struct fields, and conversion logic live in
this Script Package, not WindowsAgent Core.

## Failure and output

Application-level failure of both memory and save produces
`INVENTORY_ALL_SOURCES_FAILED`. Missing package members, platform mismatch,
undeclared alias, missing export, forged blob, FFI limit, deadline, protocol,
or process failure is terminal infrastructure failure and does not activate
another source or decoder.

The terminal job output must pass `output.schema.json`. It deliberately contains
the raw occupied backpack item contract: slot, item ID, quantity, paired item
ID, inventory key, and instance ID. It never contains the selected save path,
save bytes, raw memory, credentials, or sensitive local diagnostics.

Human validation reports, pull requests, logs, and other public evidence are a
separate privacy surface. They may include source kind, attempts, record and
occupied counts, native alias/function accounting, and Observer accounting.
They must not reproduce item records or other private gameplay data.
