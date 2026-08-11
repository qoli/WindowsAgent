# Windows Action Runtime

## Status

**Partially landed.** The game-neutral `windows-key-action-v1` runtime
implements serialized, foreground-bound keyboard presses and one non-blocking
held-key lease through Windows scan-code `SendInput`. Pointer input, chords,
and arbitrary key sequences remain deferred.

## Boundary

Each key Action package is finite, schema-bound, and owned by one executable
Rule. A package maps a schema-valid selection to one manifest-declared binding;
the caller cannot provide an undeclared physical key. The package selects one
of two binding sources:

- `literal-key-v1` declares a canonical key directly in each binding;
- `frontier-active-preset-v1` declares an Elite logical control such as
  `SetSpeed100` or `UI_Select` and resolves its physical key from the active
  Frontier preset.

Both sources use the same `windowsinput` driver. The manifest declares either
a `press` gesture or a `lease` gesture. A press has a default hold time from 1
to 1000 milliseconds. A
package may expose one schema-validated integer input as an explicit hold-time
override within manifest-declared minimum and maximum bounds; physical keys
remain non-callable input. Lease packages expose explicit `START`, `RENEW`, and
`STOP`. Only one lease is active in the controller at once. A lease normally
owns one resolved key; a compound Frontier binding may own exactly two
distinct controls for overlapping diagonal input under the same lease ID. A
package-declared one through ten second duration bounds each renewal; the
shipped Elite attitude hold uses 2500 ms. A press Action may run while a lease
remains active only when its resolved physical key differs from every held key;
an exact key conflict fails before injection. Explicit stop, expiry, streaming failure compensation,
and Agent shutdown release the same resolved key or key pair.
Literal-key packages do not require Frontier configuration; a missing Frontier
root fails only a package that explicitly selects the Frontier binding source.

For a Frontier binding, every invocation reads `StartPreset.4.start`, locates
the unique regular `.binds` file whose XML `PresetName` matches, and requires
one supported Keyboard binding for the declared logical control. A literal
binding performs no game-specific filesystem lookup. Both paths snapshot the
owning foreground process before resolution, revalidate the same PID/name/path
immediately before injection, and serialize all injected input.

The driver converts the canonical Windows key to an extended scan code with
`MapVirtualKeyW(MAPVK_VK_TO_VSC_EX)`, sends separate key-down and key-up records,
and returns `backend`, `key`, `scanCode`, `extended`, and `holdMs` evidence. A
Frontier result additionally returns the preset, binding file, and logical
control provenance.

Missing or ambiguous preset files, no/ambiguous Keyboard binding, unsupported
key names, invalid hold duration, failed scan-code mapping, foreground drift,
partial `SendInput`, cancellation, and schema errors fail explicitly. The
Windows driver attempts key release after an accepted key-down even when the
hold is cancelled. It does not choose a substitute preset, key, device, input
provider, virtual-key injection path, or window-message path.

Canonical keys currently include `Key_A` through `Key_Z`, `Key_0` through
`Key_9`, `Key_F1` through `Key_F24`, navigation keys, Space, Enter, Escape,
Tab, Backspace, Insert/Delete, and explicit left/right Shift, Control, and Alt.
Frontier's `Key_LeftArrow`, `Key_UpArrow`, `Key_RightArrow`, and
`Key_DownArrow` names resolve to the same directional Windows virtual keys as
the canonical navigation names.

The HTTP Action surface currently shares the agent's unauthenticated network
trust boundary. This is an explicit deployment limitation, not an
authorization mechanism.

The persistent host is built and deployed only as a Windows GUI-subsystem
executable. The canonical build script verifies its PE header, while both the
full installer and transactional updater reject console-subsystem candidates
before stopping the running task. The console artifact has a separate
diagnostic-only filename.

## Shipped Actions

- `elite-dangerous/ui-control`: one higher-model-selected `UP`, `DOWN`, `LEFT`,
  `RIGHT`, `SELECT`, or `BACK`; it does not autonomously navigate a menu.
- `elite-dangerous/set-throttle`: exact logical 0% or 100% throttle commands
  using the player's active binding preset.
- `elite-dangerous/ship-attitude-control`: one pitch, yaw, or roll pulse using
  the player's active binding preset and an optional bounded hold override.
- `elite-dangerous/ship-attitude-hold`: one leased pitch, yaw, or roll key hold
  with explicit start, renewal, and release evidence.

## Deferred

- pointer input, chords, multiple independent held-key leases, and arbitrary
  multi-key sequences;
- authenticated remote Action invocation and a complete durable finite-action
  lifecycle journal.
