# Elite Dangerous visual ship speed

This finite composite Action captures only the fixed speed-number area, runs
the resident PP-OCR recognizer with a digit-only CTC constraint, observes the
slashed-zero pixel topology, and applies the conservative ensemble classifier.
It returns `STOPPED` only when the zero topology is confirmed, `LOW_SPEED` for
the deliberately imprecise `1-9`
range, `MOVING` for values of at least `10`, and `UNKNOWN` for insufficient or
conflicting evidence. Only `MOVING` exposes a non-zero `speed.displayValue`;
`LOW_SPEED` retains its digit only as diagnostic `rawCandidate`. A workflow
must require repeated `STOPPED` observations before treating the ship as
stably stopped.

The Action may be registered as a Monitor by a Rule consumer. Registration is
opt-in; invoking or declaring the Action does not start a loop. A Monitor emits
the complete Action result, including `UNKNOWN`, so workflow code can compare
observed speed with input-command evidence without treating them as the same
state. The detector-based `ship-speed-text-regions` Action remains available as
diagnostic evidence but is not part of this fixed-coordinate speed path.
