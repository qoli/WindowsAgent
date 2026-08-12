# Elite Dangerous persistent Supercruise HUD state

This finite composite Action first checks the resident lower-left PP-OCR
capture for the persistent Supercruise Assist dashboard. At least two of
`DISTANCE`, `SPEED`, and `ALIGNMENT` in the same frame prove `ACTIVE`.

Before Assist owns the ship those labels are legitimately absent. The Action
then checks the independent speed display and accepts the Supercruise-only
units `km/s`, `Mm/s`, or `c` at the declared confidence thresholds. Because
this ROI is already limited to the numeric speed display, unit classification
keeps only letters and accepts `KMS`, `MMS`, or `C`. This covers PP-OCR's common
slash-elided and slash-as-digit readings such as `30.0km7s` without weakening
the detection or recognition confidence Gates. A plain
normal-space numeric speed is not accepted. It returns `INACTIVE` when neither
visual contract is present and never infers state from a prior FSD command or
a transient central prompt.
