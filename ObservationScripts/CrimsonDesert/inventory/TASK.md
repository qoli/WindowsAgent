# Crimson Desert Inventory Read

## Question

Return the raw occupied inventory records from the first valid source in this
strict order:

1. the current live `CrimsonDesert.exe` process;
2. one explicitly selected `save.save` snapshot.

If both sources fail validation, the job fails.

## Preconditions

- The bound process image SHA-256 is
  `d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f`
  (`1.0.0.2145`).
- The inventory UI is open.
- The job input `save` identifies one authorized logical-root-relative file;
  the script never chooses “latest save”.
- The registered save decoder identity is
  `crimson-rs/inventory@bb730180`.

## Observer Calls

The first attempt performs one bounded module query, one module-range signature
scan, one RIP-relative resolution, finite pointer/header reads, and one
strided record read. The record stride is `0xC8`; the raw item ID, quantity,
and paired ID fields are read at offsets `0x08`, `0x10`, and `0x90`.

Only when that attempt returns a typed source failure, the script calls
`file.decode` once for the selected save. The decoder must verify the save
container, decrypt, authenticate, decompress, deserialize, and enumerate the
active character inventory before returning normalized records.

It performs no write, hook, debugger attach, thread suspension, injection,
timer, watch, retry, implicit file selection, OCR, or third-source lookup.

## Output

The output is one JSON value. `source.kind` states whether the result came from
`process-memory` or `save-file`; `attempts` makes the fallback visible.
`recordCount` is the bounded source count. `occupiedCount` counts records whose
raw primary item ID is nonzero. Each item contains its slot, raw item ID,
unsigned quantity, and source-supported identity fields. Unsupported
source-specific fields are explicitly null.

For a save result, `saveModifiedAt` is the file timestamp and therefore the
freshness boundary. Save data is not described as current live state. No
localized name, rarity, description, category, icon, or market value is
inferred.

## Failure

Unsupported executable identity, missing or ambiguous signature, unreadable
pointer-chain step, and invalid memory header are eligible to proceed to the
save attempt. Save container authentication, decryption, decompression,
deserialization, or inventory enumeration failure makes that source attempt
invalid.

Missing save input, path authorization, permission, protocol, deadline,
step/output limit, and package identity failures are infrastructure or contract
failures and terminate immediately. When both source attempts fail, the
terminal code is `INVENTORY_ALL_SOURCES_FAILED` and identifies both source
error codes.

The live 2026-07-27 verification found the signature but the static pointer
chain was not readable even with the inventory UI open. That is the concrete
condition for which the user-authorized save fallback exists.

The fallback is fixture-verified only until the pinned `crimson-rs` decoder is
packaged and registered in WindowsAgent. An unknown or absent decoder fails;
the observer never substitutes a different parser.

## Privacy And Retention

Only the schema-accepted raw inventory result and bounded provenance are
eligible for persistence. Raw process buffers, memory dumps, screenshots, and
interpreter state are not output and must not be retained by the job.
