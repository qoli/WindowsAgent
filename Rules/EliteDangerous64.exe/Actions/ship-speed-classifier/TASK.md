# Elite Dangerous visual ship speed classifier

Classify one raw PP-OCR box set from the reviewed speed-number region. A value
is returned only when the box occupies the calibrated HUD geometry, its
physical aspect agrees with the recognized digit count, and recognition meets
the reviewed confidence rule. OCR strings containing the known ambiguous
digit `8` are intentionally unresolved by this first contract because the
review corpus contains high-confidence `0/8` and `6/8` substitutions.

`displayValue` is the number visibly printed by the HUD. `unit` remains null:
this Action does not infer normal-flight or supercruise units from an unseen
source. Missing, covered, malformed, or ambiguous evidence returns
`state=UNKNOWN`; there is no journal, status-file, command-state, or temporal
fallback.
