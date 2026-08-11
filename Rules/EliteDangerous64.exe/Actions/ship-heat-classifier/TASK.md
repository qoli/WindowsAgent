# Ship heat classifier

This pure Action accepts the complete OCR result. Its primary HUD format Gate
accepts a raw two- or three-digit value followed by `%` only at confidence
0.80 or higher. This avoids the digits-only decoder turning a clearly observed
percent sign into a hallucinated trailing `8`. When that exact raw HUD format
is absent, the existing constrained two- or three-digit confidence and
raw/constrained agreement Gates apply. It preserves all other input as
`UNKNOWN`; it does not retain a prior temperature or decide a flight-safe
threshold.
