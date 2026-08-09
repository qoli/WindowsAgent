# Elite Dangerous visual ship speed classifier

Classify constrained PP-OCR evidence together with the dedicated slashed-zero
glyph observation from the reviewed speed-number region. `STOPPED` is owned by
the pixel topology and does not require OCR confidence. Non-zero candidates are
accepted only when the worker confirms it used digit-constrained
CTC decoding, the result contains one through four digits, constrained
confidence is at least `0.55`, and the unrestricted-versus-constrained
confidence margin is at most `0.12`.

The full character dictionary remains active for the unrestricted candidate.
This means a visually competing letter such as `V` is retained as evidence;
the digit candidate is accepted only when forcing the digit alphabet did not
materially weaken the model score. All digits, including `8`, are eligible.

The result deliberately uses task-level speed bands instead of claiming that
single-digit OCR is exact:

- `STOPPED`: the independent pixel topology confirms ED's dense slashed-zero
  glyph and no qualified multi-digit OCR observation contradicts it. The
  returned diagnostic candidate is normalized to zero. Temporal consumers
  must still require repeated `STOPPED` observations before completing a stop
  Gate.
- `LOW_SPEED`: the candidate is `1` through `9`. `displayValue` is null because
  digits in this HUD range can be confused with each other; the pixel topology
  must also reject the slashed zero. `rawCandidate` retains the diagnostic OCR
  candidate.
- `MOVING`: the candidate is at least `10`, and `displayValue` exposes that
  concrete number.
- `UNKNOWN`: evidence is missing, malformed, weak, conflicting, or the glyph
  topology is unresolved; both values are null.

`unit` remains null: this Action does not infer normal-flight or supercruise
units from an unseen source. There is no journal, status-file, command-state,
or low-confidence temporal fallback. The glyph observer and OCR are declared
parts of one ensemble; neither is a hidden alternate path.
