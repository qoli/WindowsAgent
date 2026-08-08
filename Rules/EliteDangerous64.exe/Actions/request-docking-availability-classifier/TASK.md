# Elite Dangerous Request Docking availability classifier

Use dynamically detected and perspective-rectified OCR text lines to locate
`REQUEST DOCKING` or `CANCEL DOCKING`. The adjacent pixels captured in the same
frame determine whether the located action row is focused or merely visible.

Weak or contradictory text and color evidence returns `UNKNOWN`. A settled
broad detection result with other confident panel text but no plausible
Request/Cancel candidate returns `UNAVAILABLE`; an empty or weak action area
remains `UNKNOWN`. No previous sample or ScreenParser result is substituted.

This Action reports the button state only. A caller must separately require the
request-docking range Gate before sending `SELECT`.
