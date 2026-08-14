# Classify Elite Dangerous flight prompt text (internal)

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
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET` is the distinct
`SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` Gate. It means the selected
destination is occluded and the Assist computer cannot continue the direct
approach. It does not authorize `align-visible-target`, prove a safe bypass
direction, or mean Assist was cancelled. An owning Streaming Action must stop
the direct approach, obtain fresh target-focus geometry, clear the obstruction,
then re-read this prompt before restoring the Assist blue-zone throttle.
The live `THROTTLE UP TO ENGAGE` prompt is
`FSD_THROTTLE_UP_REQUIRED`: it proves the FSD reached its charged throttle
handoff, but does not prove that Supercruise entry occurred.
`ALIGN WITH TARGET DESTINATION` maps to the historically named
`FSD_ALIGNMENT_REQUIRED`, but the prompt is not proof that an FSD charge is in
progress. Elite Dangerous also displays it after Supercruise Assist has been
selected while the destination is not aligned. Owning workflows must interpret
this current-frame target-alignment Gate in their own phase and must verify the
prompt disappears after correction; this finite classifier performs neither
alignment nor multi-frame disappearance confirmation.
Missing, unrelated, ambiguous, or
low-confidence content remains `UNKNOWN`; never choose a status merely because
it is the closest catalog phrase.

The raw prompt is bounded to 128 characters because the upstream OCR Action
reads one narrow, single-line HUD region. This also keeps edit-distance work
bounded when unrelated screen text reaches the OCR output.

This Rule-internal Action also returns the small generic `routeDecision`
interface consumed by the same-capture OCR cascade. `routeDecision.accepted`
and `routeDecision.state` are derived from the same result as
`flightStatus`—they are not a second classifier or threshold set.

This Action performs no screen capture, OCR inference, event emission, or
follow-up execution. It preserves the raw text and OCR confidence in its
output. Malformed raw OCR input fails schema validation explicitly.
