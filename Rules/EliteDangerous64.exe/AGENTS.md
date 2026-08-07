# Elite Dangerous Agent Rule

Use this Rule only when a fresh WindowsAgent capture reports:

- `foreground.executable_name: EliteDangerous64.exe`
- `rule.status: matched`
- `rule.id: EliteDangerous64.exe`

## Current capability boundary

Read the exact `rule.actions.url` and `rule.registrations.url` from the capture
before using a game-specific capability.

`elite-dangerous/compass` is a finite, directly callable Action. It owns a
fixed compass region in the 1920x1080 reference coordinate space and its HUD
color interpretation. The game-neutral Observer maps that region through a
centered 16:9 viewport and returns reference-density pixels. A changed
foreground process or invalid compass evidence fails explicitly.

`elite-dangerous/flight-prompt-text` is a second finite Action. It captures the
reviewed central prompt as a 400x40 reference-density RGB line and sends it to
the Rule-declared `ocr/w480` resident DirectML worker. Its output is
raw OCR text evidence only.

`elite-dangerous/flight-status` is its finite, pure post-processing Action. It
accepts the complete raw OCR output and combines OCR confidence with reviewed
phrase similarity. It returns `SUPERCRUISE`, `AUTO_LAUNCH`,
`WAITING_IN_QUEUE`, `FSD_CHARGING`, `FSD_ALIGNMENT_REQUIRED`, `AUTO_DOCK`, or
evidence-preserving `UNKNOWN`. It performs no capture or OCR and malformed raw
input fails schema validation. Multi-frame confirmation, event emission, and
follow-up execution remain registration concerns.

`elite-dangerous/ship-status` is a finite composite Action over the reviewed
lower-right HUD region. Its internal raw Action captures at reference density
and uses the Rule-declared `ocr/text-regions` resident DirectML worker to return
PP-OCR quadrilateral boxes and recognition evidence. A pure classifier confirms
only `MASS`, `LANDING`, and `CARGO`, then independently returns `massLock`,
`landingGear`, and `cargoScoop` as cyan `ON`, orange `OFF`, or evidence-preserving
`UNKNOWN`. It never falls back to the retired fixed-position triplet detector.

Target geometry uses 1080p reference pixels. `screenAngleDegrees` is clockwise
from straight up. `centerZone.inside` is a current-frame circular membership
state; do not infer an entered/exited transition without Monitor history.

These four observation Actions declare that they may be registered as either a
Monitor or Reaction, but the registration catalog is intentionally empty. Do not infer a
timer or event subscription from `registrableAs`; declaring eligibility does
not activate a registration. Likewise, `ocr/w480` and `ocr/text-regions`
residency only keep model initialization alive while this Rule owns the
foreground; neither invokes an OCR Action nor produces an event.

`elite-dangerous/ui-control` is a finite slow-interaction primitive. A
supervising model chooses exactly one logical `UP`, `DOWN`, `LEFT`, `RIGHT`,
`SELECT`, or `BACK` after inspecting a fresh screenshot. The runtime resolves
that logical control from the game's active binding preset, then uses the
game-neutral Windows scan-code input driver; never assume Space or any other
fixed physical key. Successful output includes the binding source, backend,
scan code, extended-key flag, and configured hold time.

### Visual focus confirmation for the higher execution Agent

Do not infer keyboard focus from one color or one static frame. Elite
Dangerous uses its configured HUD color throughout the interface, so orange or
bright-yellow appearance alone is not a focus rule. Before navigating, the
higher execution Agent must first identify and calibrate the current menu's
highlight state from a fresh screenshot and a reversible, one-step visual
test. Relevant evidence may include a changed fill, border, brightness, text
contrast, glyph treatment, or contextual label, but only a change synchronized
with a completed directional input establishes which treatment means focused
in the current UI state.

Focus is not confined to the vertical text menu. It may be on the icon row
above it, so calibration must include both areas. In one reviewed default-color
station sequence, the `RETURN TO SURFACE` elevator icon initially had a filled
tile. One `UP` produced no visible movement at that boundary. One subsequent
`DOWN` moved the differing tile treatment to `STARPORT SERVICES`; a second
`DOWN` moved it to `AUTO LAUNCH`. This sequence demonstrates how to calibrate
focus through correlated movement; its bright-yellow color is an observation,
not a reusable rule.

Use the following evidence loop before `SELECT`:

1. Capture a fresh frame and describe the candidate highlight treatments,
   including the icon row as well as the text rows. Do not yet declare which
   candidate is focused from color alone.
2. Send exactly one reversible directional `ui-control` invocation, wait for
   it to return, then capture a new frame. Associate the new frame only with
   that completed invocation.
3. Compare the two frames and identify whether one coherent highlight treatment
   moved from one control to an adjacent control. Use that synchronized visual
   delta to calibrate the focused and unfocused appearances for the current
   menu state.
4. Navigate one direction and one fresh frame at a time. Invoke `SELECT` only
   when the target has the calibrated focused appearance in the newest frame.

A completed `ui-control` result proves only that WindowsAgent resolved and
injected the configured key. It does not prove that the game accepted the key
or moved focus. If the new frame is unavailable, a capture times out, multiple
inputs could fall between frames, or no focus delta is visible, report focus as
`UNKNOWN`; do not reuse an older frame and do not invoke `SELECT`. After
`SELECT`, treat the item as activated only after a fresh visual transition or
the relevant observation Action provides matching evidence. Input completion
alone is not activation or workflow completion.

`elite-dangerous/set-throttle` is deterministic. It accepts only `0` or `100`,
resolves the corresponding logical throttle control from the active preset on
each invocation, and reports the exact resolved key and injection evidence.

`elite-dangerous/leave-station` is an interruptible linear Streaming Action.
Invoke it with `{"stationConfirmed":true}` only after high-level visual
confirmation that the ship is inside a station, then follow the returned NDJSON
watch URL. While its phase is `AWAITING_AUTO_LAUNCH`, arrange Auto Launch slowly
with the visual focus evidence loop above. Once Auto Launch is observed, the
workflow owns the 100% throttle, Mass Lock OFF, and 0% throttle sequence and
reports `COMPLETED`, `FAILED`, or `CANCELLED` through the stream.
