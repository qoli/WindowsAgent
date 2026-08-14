# Read Elite Dangerous flight status

Capture the current narrow flight-prompt region through
`elite-dangerous/flight-prompt-text`, then classify the fresh OCR result through
the Rule-internal `elite-dangerous/flight-status-classifier`.

This is the public semantic boundary. Callers pass an empty input object and
receive one fail-closed status plus the current raw text, normalized text, OCR
confidence, and decision evidence. Callers must not manually reconstruct the
OCR-to-classifier pipeline. OCR, schema, process, and capture failures remain
terminal; only ambiguous domain evidence becomes `UNKNOWN`.

This Action is a single-sample observation. A Streaming Action still owns
debounce, phase interpretation, disappearance confirmation, and any control
response.
