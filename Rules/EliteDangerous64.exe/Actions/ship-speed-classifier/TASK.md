# Elite Dangerous visual ship speed classifier

Classify one fixed-region PP-OCR result from the reviewed speed-number region.
A value is returned only when the worker confirms it used digit-constrained
CTC decoding, the result contains one through four digits, constrained
confidence is at least `0.55`, and the unrestricted-versus-constrained
confidence margin is at most `0.12`.

The full character dictionary remains active for the unrestricted candidate.
This means a visually competing letter such as `V` is retained as evidence;
the digit candidate is accepted only when forcing the digit alphabet did not
materially weaken the model score. All digits, including `8`, are eligible.

`displayValue` is the number visibly printed by the HUD. `unit` remains null:
this Action does not infer normal-flight or supercruise units from an unseen
source. Missing, covered, malformed, weak, or conflicting evidence returns
`state=UNKNOWN`; there is no journal, status-file, command-state, or temporal
fallback.
