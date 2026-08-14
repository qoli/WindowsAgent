# Read Elite Dangerous flight status

Capture and classify the current narrow flight-prompt region through
`elite-dangerous/flight-prompt-text`. That child owns one same-frame explicit
OCR cascade and returns the selected output of the Rule-internal
`elite-dangerous/flight-status-classifier`.

This is the public semantic boundary. Callers pass an empty input object and
receive one fail-closed status plus the selected raw text, normalized text, OCR
confidence, semantic decision, executed route summaries, Gate evidence,
transitions, selected route, terminal reason, model/provider provenance, and
timing. The semantic decision also reports whether catalog similarity or the
explicit `ALIGN WITH` prefix rule accepted the sample. Callers must not
manually reconstruct the OCR cascade. OCR, schema,
process, capture, preprocessing, or classifier failures remain terminal; only
ambiguous domain evidence becomes `UNKNOWN`.

This Action is a single-sample observation. A Streaming Action still owns
debounce, phase interpretation, disappearance confirmation, and any control
response.
