# Elite Dangerous Request Docking availability

This finite composite first proves that CONTACTS is selected, then detects all
text in a broad detail-panel area. The classifier anchors the action zone to
the current-frame `FACTION` box, then uses the matched Request/Cancel line's
same-frame left context to distinguish `AVAILABLE`, `FOCUSED`, `UNAVAILABLE`,
`DOCKING_ACTIVE`, and evidence-preserving `UNKNOWN`. The same broad OCR result
also returns `DENIED` when it contains a high-confidence
`DOCKING REQUEST DENIED.` notification; this proves rejection without inferring
its cause or the button's current focus/availability. No second capture or
fixed action-row coordinate is used.

The selected Contacts target is not assumed to support docking. This Action
does not navigate, change focus, send `SELECT`, or prove the independent
`distance < 7.5 km` Gate. Only `FOCUSED` plus an allowed range is safe input to
a later Request Docking control Action.
