# Elite Dangerous Request Docking availability classifier

Use dynamically detected and perspective-rectified OCR text lines to first
locate the stable `FACTION` heading, then accept `REQUEST DOCKING` or
`CANCEL DOCKING` only inside its bounded relative action zone. This same-frame
anchor alignment follows the broad-search pattern used by
`request-docking-range` and tolerates whole-panel HUD drift without a second
capture. The adjacent pixels captured in that frame determine whether the
located action row is focused or merely visible.

Missing or ambiguous `FACTION` evidence returns `UNKNOWN`; no fixed action-row
coordinate is substituted. Weak or contradictory text and color evidence
returns `UNKNOWN`. A settled
broad detection result with other confident panel text but no plausible
Request/Cancel candidate returns `UNAVAILABLE`; an empty or weak action area
remains `UNKNOWN`. No previous sample or ScreenParser result is substituted.

This Action reports the button state only. A caller must separately require the
request-docking range Gate before sending `SELECT`.
