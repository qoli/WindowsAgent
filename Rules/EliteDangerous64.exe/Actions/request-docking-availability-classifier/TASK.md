# Elite Dangerous Request Docking availability classifier

Use dynamically detected and perspective-rectified OCR text lines to first
locate the stable `FACTION` heading, then accept `REQUEST DOCKING` or
`CANCEL DOCKING` only inside its bounded relative action zone. This same-frame
anchor alignment follows the broad-search pattern used by
`request-docking-range` and tolerates whole-panel HUD drift without a second
capture. The adjacent pixels captured in that frame determine whether the
located action row is focused or merely visible.

The same OCR result also detects the short-lived, explicit
`DOCKING REQUEST DENIED.` notification. A high-confidence denial phrase returns
`DENIED` when there is no stronger same-frame post-submit state. A separately
anchored and accepted `CANCEL DOCKING` row proves that the request is active
and therefore overrides the notification; the decision records both the
detected conflict and the override. A still-visible `REQUEST DOCKING` row does
not override denial. The message never infers a cause such as ship size, range,
reputation, or pad availability.

The focused-fill threshold is calibrated against reviewed settled 4K/HDR
samples whose dynamic left-context bright ratios were `0.0892` and `0.0934`.
Ratios below `0.08` do not prove focus; the measured bright and dark ratios
remain in the output so live behavior can be audited without relying on the
threshold alone. The non-focused row uses a separate dim red-orange predicate,
calibrated from the reviewed native sample around RGB `(48, 11, 0)`; this can
prove `AVAILABLE`, but never `FOCUSED`.

Missing or ambiguous `FACTION` evidence returns `UNKNOWN`; no fixed action-row
coordinate is substituted. Weak or contradictory text and color evidence
returns `UNKNOWN`. A settled
broad detection result with other confident panel text but no plausible
Request/Cancel candidate returns `UNAVAILABLE`; an empty or weak action area
remains `UNKNOWN`. No previous sample or ScreenParser result is substituted.

This Action reports the button state only. A caller must separately require the
request-docking range Gate before sending `SELECT`.
