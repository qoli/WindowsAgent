# Ship heat classifier

This pure Action accepts the complete OCR result. Its primary HUD format Gate
accepts a raw two- or three-digit value followed by `%` only at confidence
0.75 or higher. Ten consecutive live samples of a displayed `23%` all retained
that exact raw syntax at confidence 0.77999 through 0.85745 while the constrained
decoder returned `238` every time. This calibrated Gate avoids the digits-only decoder turning a clearly observed
percent sign into a hallucinated trailing `8`. When that exact raw HUD format
is present below the confidence Gate, the result is `UNKNOWN`; it never falls
through to a contradictory digits-only candidate. When that exact format is
absent, the result is also `UNKNOWN`: live samples showed both raw and
constrained decoders returning `238` for a visible `23%`, so a digits-only
candidate cannot honestly distinguish a hallucinated suffix from real
three-digit heat. Both decodings remain diagnostic evidence. The classifier
does not retain a prior temperature or decide a flight-safe threshold.
