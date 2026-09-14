# Controller-to-Pointer Runtime

## Current Status

**Draft.** The repository now contains a pure, side-effect-free
`internal/controllerpointer` mapper with focused tests. It implements radial
dead-zone removal, an explicit response curve, elapsed-time-based velocity,
optional Y inversion, and fractional pixel carry.

No controller adapter, Windows pointer-movement adapter, resident activation
listener, host process, configuration surface, deactivation contract, button
mapping, deployment path, or signed-in Windows acceptance exists yet. The existing
`windows-pointer-action-v1` runtime supports finite left clicks only and does
not implement this loop.

## Problem

A controller-to-pointer capability must first recognize one cross-controller
activation chord: the semantic system Guide/Home/PS button plus the semantic
right-thumbstick button (commonly called R3). Once active, it must continuously
sample an analog stick and emit relative mouse movement without turning
controller policy into a generic `windowsinput` driver concern. It must also
stop predictably when cancelled, when the selected controller disconnects, or
when its owning foreground policy no longer holds.

The capability is continuous and stateful: it retains fractional movement,
needs explicit lifecycle ownership, and may later own button-down state. It is
therefore not a sequence of finite pointer Actions.

## Scope

This design owns a game-neutral Windows runtime that converts one explicitly
selected controller's state into relative pointer movement in the signed-in
interactive desktop.

It does not own game-specific bindings, aim assistance, target selection,
screen interpretation, controller emulation, virtual HID installation, process
launch, foreground activation, or remote desktop control.

## Module And Seams

`internal/controllerpointer` is the owning deep module. Its eventual external
interface should start one cancellable mapping run from a complete validated
configuration and return one terminal result. Polling cadence, hot-plug state,
stick normalization, dead-zone math, curve application, fractional carry,
button edge detection, pointer injection, cleanup, and evidence stay behind
that interface.

Two internal seams isolate platform behavior:

- a controller-source adapter uses GameInput to report one stable device
  identity, semantic Guide system-button transitions, normalized gamepad state,
  semantic right-thumbstick-button state, physical labels, and timestamps;
- a pointer-sink adapter emits relative movement and button transitions through
  Windows `SendInput`.

The activation detector matches semantic controls, not device-specific strings:
`GameInputSystemButtonGuide` plus `GameInputGamepadRightThumbstick`. Labels such
as PS, Xbox Guide, Home, R3, or right-stick button are evidence and UI text only.
Because GameInput reports the Guide key through a system-button callback rather
than the ordinary gamepad button bitset, the module must correlate both states
for the same device and define a bounded simultaneity rule before implementation.

The shipped pure mapper is below those seams. It accepts normalized samples and
elapsed time, and returns relative integer motion plus the post-curve vector.
It deliberately provides no defaults: dead zone, curve exponent, maximum
pixels per second, and Y direction must all be chosen by configuration.

## Intended Runtime Contract

The first end-to-end version should:

1. start a resident inactive listener through an explicit host lifecycle;
2. select one stable GameInput device identity rather than a transient slot;
3. activate only after that device produces the semantic Guide-plus-R3 chord;
4. sample only that controller while active and connected;
5. map one explicit stick through the configured mapper;
6. inject relative pointer movement into the interactive desktop;
7. optionally map explicitly declared controller buttons to mouse buttons;
8. expose inactive, activating, active, stopping, completed, and failed
   lifecycle evidence;
9. release every injected mouse button during cancellation and failure cleanup;
10. fail explicitly on missing Guide/R3 capability, controller loss,
   input-provider failure, pointer
   injection failure, or violated foreground policy.

Controller loss must not silently select another device. A missing or
unavailable GameInput system-button path must not fall back to undocumented
XInput exports, guessed Raw HID reports, label-string matching, or another
controller provider. Pointer injection must not fall back to `SetCursorPos`,
window messages, another desktop, or a virtual HID provider. A later design may
add an explicitly visible reconnection or provider policy, but it is not part of
the first contract.

## Evidence

Run evidence should remain privacy-minimized and include:

- capability and implementation version;
- selected stable controller identity, input provider, and reported physical
  labels for the semantic Guide and right-thumbstick buttons;
- activation-chord timestamps and outcome;
- configured stick, dead zone, curve, speed, and Y direction;
- start and terminal timestamps plus terminal reason;
- sample, emitted-movement, and button-transition counts;
- the exact failing layer and native error when execution fails.

It should not persist raw per-sample stick input by default.

## Relationship To Existing Designs

- [Windows Action Runtime](windows-action-runtime.md) owns finite Rule Actions
  and the existing click-only pointer runtime; it does not own this continuous
  mapper.
- [Streaming Action Runtime](streaming-action-runtime.md) provides relevant
  cancellation and terminal-event semantics. Whether this capability is hosted
  as a Rule-owned loop Action or an independent companion process remains an
  open product decision.
- The Action OSD may project lifecycle state only if the capability becomes a
  Rule-owned Streaming Action.

## Open Questions

- What starts and owns the resident inactive listener before the controller
  chord can be detected?
- Is activation permitted in the background through an explicit GameInput focus
  policy, or only while an owning executable remains foreground?
- Does Guide plus R3 toggle the capability off as well, or is deactivation a
  distinct chord/action?
- What simultaneity window or press-order rule defines the activation chord?
- Should the left or right stick drive the pointer?
- Which controller buttons, if any, map to left, right, middle, or wheel input?
- Which concrete controller models and connection modes must pass the first
  GameInput acceptance matrix?
- Should the lifecycle be a Rule-owned loop Streaming Action or an independent
  user-launched companion process?

## Suggested Next Steps

1. Resolve resident ownership, background focus policy, chord timing, and
   deactivation semantics.
2. Add a GameInput adapter with synthetic contract tests for device identity,
   Guide callbacks, R3 readings, capability absence, and disconnect errors.
3. Add a relative `SendInput` pointer adapter with structure-layout and partial
   insertion tests.
4. Compose the adapters behind one cancellable module interface and add Windows
   interactive acceptance for motion, cancellation, disconnect, and cleanup.
