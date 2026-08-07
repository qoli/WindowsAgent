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

`elite-dangerous/set-throttle` is deterministic. It accepts only `0` or `100`,
resolves the corresponding logical throttle control from the active preset on
each invocation, and reports the exact resolved key and injection evidence.

`elite-dangerous/leave-station` is an interruptible linear Streaming Action.
Invoke it with `{"stationConfirmed":true}` only after high-level visual
confirmation that the ship is inside a station, then follow the returned NDJSON
watch URL. While its phase is `AWAITING_AUTO_LAUNCH`, arrange Auto Launch slowly
with fresh screenshots and one `ui-control` call at a time. Once Auto Launch is
observed, the workflow owns the 100% throttle, Mass Lock OFF, and 0% throttle
sequence and reports `COMPLETED`, `FAILED`, or `CANCELLED` through the stream.
