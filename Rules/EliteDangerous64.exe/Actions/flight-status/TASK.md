# Classify Elite Dangerous flight prompt text

Accept the complete raw output of `elite-dangerous/flight-prompt-text` and map
its imperfect OCR text into one finite flight status. Normalize only ASCII
letters and digits, compare against the reviewed finite phrase catalog using
Levenshtein similarity, and combine that similarity with the OCR confidence.

Accept a status only when its combined confidence is at least `0.30` and its
lead over the runner-up is at least `0.10`. These constants separate all 13
reviewed positive images from all 15 reviewed unknown or interfering images in
the 28-image, five-pass w480 calibration set. Missing, unrelated, ambiguous, or
low-confidence content remains `UNKNOWN`; never choose a status merely because
it is the closest catalog phrase.

The raw prompt is bounded to 128 characters because the upstream OCR Action
reads one narrow, single-line HUD region. This also keeps edit-distance work
bounded when unrelated screen text reaches the OCR output.

This Action performs no screen capture, OCR inference, event emission, or
follow-up execution. It preserves the raw text and OCR confidence in its
output. Malformed raw OCR input fails schema validation explicitly.
