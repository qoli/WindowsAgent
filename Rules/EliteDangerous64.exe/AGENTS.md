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

`elite-dangerous/ship-status` is a finite, non-OCR Action over the reviewed
lower-right HUD region. It locates the three-row status group and independently
returns `massLock`, `landingGear`, and `cargoScoop`. Each indicator reports
`ON` for cyan filled, `OFF` for orange hollow, or `UNKNOWN` when the complete
group cannot be established; its `on` field is strictly paired as `true`,
`false`, or `null`.

Target geometry uses 1080p reference pixels. `screenAngleDegrees` is clockwise
from straight up. `centerZone.inside` is a current-frame circular membership
state; do not infer an entered/exited transition without Monitor history.

All four Actions declare that they may be registered as either a Monitor or
Reaction, but the registration catalog is intentionally empty. Do not infer a
timer or event subscription from `registrableAs`; declaring eligibility does
not activate a registration. Likewise, `ocr/w480` residency only keeps model
initialization alive while this Rule owns the foreground; it does not invoke
the OCR Action or produce an event.
