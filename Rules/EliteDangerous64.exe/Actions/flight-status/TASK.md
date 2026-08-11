# Classify Elite Dangerous flight prompt text

Accept the complete raw output of `elite-dangerous/flight-prompt-text` and map
its imperfect OCR text into one finite flight status. Normalize only ASCII
letters and digits, compare against the reviewed finite phrase catalog using
Levenshtein similarity, and combine that similarity with the OCR confidence.

Accept a status only when its phrase similarity is at least `0.60`, its
combined OCR-confidence score is at least `0.30`, and its lead over the
runner-up is at least `0.10`. The similarity floor rejects unrelated text even
when the OCR engine is confident: the reviewed `CURIULIS STARP` interference
previously scored `0.342946` against `FSD_CHARGING`, but its phrase similarity
is only `0.40`. It now remains `UNKNOWN`.

This fail-closed boundary retains 12 clear positive images and all 15 reviewed
unknown or interfering images in the original 28-image, five-pass w480
calibration set. One heavily corrupted FSD image, `RESHAGINGORT`, deliberately
changes from `FSD_CHARGING` to `UNKNOWN` because its phrase similarity is only
`0.4167`; the following clear `PRESS TO ABORT` image remains accepted. The
later reviewed Auto Dock capture adds `SLOW DOWN FOR AUTO DOCK` as the finite
`SLOW_DOWN_FOR_AUTO_DOCK` state. The reviewed central Supercruise exit prompt
is classified as `SAFE_DISENGAGE_READY`; a flight workflow must still require
multi-frame confirmation before sending the FSD command. `SUPERCRUISE ASSIST
ACTIVE` is a separate `SUPERCRUISE_ASSIST_ACTIVE` state rather than ordinary
`SUPERCRUISE`, allowing an owning workflow to stop issuing attitude and
throttle inputs after the game computer takes control. `ALIGN WITH ESCAPE
VECTOR` is classified separately as `FSD_ESCAPE_VECTOR_REQUIRED`; a workflow
must not infer that Compass ownership changed from `FSD_CHARGING` alone.
Missing, unrelated, ambiguous, or
low-confidence content remains `UNKNOWN`; never choose a status merely because
it is the closest catalog phrase.

The raw prompt is bounded to 128 characters because the upstream OCR Action
reads one narrow, single-line HUD region. This also keeps edit-distance work
bounded when unrelated screen text reaches the OCR output.

This Action performs no screen capture, OCR inference, event emission, or
follow-up execution. It preserves the raw text and OCR confidence in its
output. Malformed raw OCR input fails schema validation explicitly.
