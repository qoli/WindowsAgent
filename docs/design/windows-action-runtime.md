# Windows Action Runtime

## Status

**Partially landed.** The `frontier-key-action-v1` runtime implements one
serialized, foreground-bound keyboard press for Elite Dangerous. General
pointer input, held keys, chords, and arbitrary key sequences remain deferred.

## Boundary

Each input Action package is finite, schema-bound, and owned by one executable
Rule. It maps a schema-valid selection to an Elite logical control such as
`SetSpeed100` or `UI_Select`; the caller cannot supply a physical key.

For every invocation the runtime reads `StartPreset.4.start`, locates the
unique regular `.binds` file whose XML `PresetName` matches, and requires one
supported Keyboard binding for the declared logical control. It snapshots the
owning foreground process before resolution, revalidates the same PID/name/path
immediately before injection, and serializes all injected input. A successful
call sends one `SendInput` key-down/key-up pair and returns the resolved preset,
file, logical control, and physical key.

Missing or ambiguous preset files, no/ambiguous Keyboard binding, unsupported
key names, foreground drift, partial `SendInput`, cancellation, and schema
errors fail explicitly. The Windows sender attempts key release if Windows
accepts only the key-down event. It does not choose a substitute preset, key,
device, or input provider.

The HTTP Action surface currently shares the agent's unauthenticated network
trust boundary. This is an explicit deployment limitation, not an
authorization mechanism.

## Shipped Actions

- `elite-dangerous/ui-control`: one higher-model-selected `UP`, `DOWN`, `LEFT`,
  `RIGHT`, `SELECT`, or `BACK`; it does not autonomously navigate a menu.
- `elite-dangerous/set-throttle`: exact logical 0% or 100% throttle commands
  using the player's active binding preset.

## Deferred

- general logical-control package format beyond Frontier bindings;
- pointer input, chords, held-key leases, and multi-key sequences;
- authenticated remote Action invocation and a complete durable finite-action
  lifecycle journal.
