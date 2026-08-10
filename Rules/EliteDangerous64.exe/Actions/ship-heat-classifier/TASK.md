# Ship heat classifier

This pure Action accepts the complete constrained OCR result and returns the
current displayed heat percentage only when two or three digits satisfy the
confidence and raw/constrained agreement Gates. It preserves all other input
as `UNKNOWN`; it does not retain a prior temperature or decide a flight-safe
threshold.
