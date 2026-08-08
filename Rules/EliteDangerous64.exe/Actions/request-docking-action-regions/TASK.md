# Elite Dangerous Contacts action text regions

Detect text lines in the broad Contacts action area. The resident detector
locates each current quadrilateral and rectifies that individual line before
recognition, so cockpit-view movement does not require a fixed action-row crop.

This raw Action does not decide whether Request Docking exists or is focused.
It returns current boxes, text, confidence, and adjacent pixels as evidence.
