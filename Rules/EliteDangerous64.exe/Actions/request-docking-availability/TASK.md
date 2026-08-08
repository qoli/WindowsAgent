# Elite Dangerous Request Docking availability

This finite composite first proves that CONTACTS is selected, then detects all
text in a broad action area. The detector dynamically locates each current text
quadrilateral and rectifies it before recognition. The classifier uses the
matched line's same-frame left context to distinguish `AVAILABLE`, `FOCUSED`,
`UNAVAILABLE`, `DOCKING_ACTIVE`, and evidence-preserving `UNKNOWN`.

The selected Contacts target is not assumed to support docking. This Action
does not navigate, change focus, send `SELECT`, or prove the independent
`distance < 7.5 km` Gate. Only `FOCUSED` plus an allowed range is safe input to
a later Request Docking control Action.
