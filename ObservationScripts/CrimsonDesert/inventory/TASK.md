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
- The save root and relative file are explicitly supplied by the user.
- The package-native artifact is
  `native/windows-amd64/crimson-rs.inventory.bb730180.dll`.
- Its manifest alias is `save-decoder` and its SHA-256 is
  `c3acb8368369a856c8e65ea546ad6a3c2147cef852f9eff79cb3869e6d97272c`.

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
6. queries count, reads one fixed record array, filters `inventoryKey = 2`,
   converts to JSON, and frees the save handle.

All Crimson exports, return codes, struct fields, and conversion logic live in
this Script Package, not WindowsAgent Core.

## Failure and output

Application-level failure of both memory and save produces
`INVENTORY_ALL_SOURCES_FAILED`. Package digest failure, platform mismatch,
undeclared alias, missing export, forged blob, FFI limit, deadline, protocol,
or process failure is terminal infrastructure failure and does not activate
another source or decoder.

The terminal JSON must pass `output.schema.json`. Reports may include source
kind, attempts, counts, native alias/function accounting, and Observer
accounting. They must not contain private save paths, save bytes, item details,
raw memory, or sensitive local diagnostics.
