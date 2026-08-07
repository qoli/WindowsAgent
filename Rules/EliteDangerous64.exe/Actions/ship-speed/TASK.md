# Elite Dangerous visual ship speed

This finite composite Action captures only the fixed speed-number area, runs
the resident PP-OCR recognizer with a digit-only CTC constraint, and applies
the conservative visual classifier to the same frame. It reports the visible HUD number as
`speed.displayValue` only when `speed.state` is `KNOWN`.

The Action may be registered as a Monitor by a Rule consumer. Registration is
opt-in; invoking or declaring the Action does not start a loop. A Monitor emits
the complete Action result, including `UNKNOWN`, so workflow code can compare
observed speed with input-command evidence without treating them as the same
state. The detector-based `ship-speed-text-regions` Action remains available as
diagnostic evidence but is not part of this fixed-coordinate speed path.
