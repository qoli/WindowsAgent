# Elite Dangerous persistent Supercruise HUD state

This finite composite Action reuses the resident lower-left PP-OCR text-region
capture and classifies the persistent Supercruise dashboard. At least two of
`DISTANCE`, `SPEED`, and `ALIGNMENT` must be independently recognized in the
same frame at the declared confidence thresholds. It returns `INACTIVE` when
that evidence is absent and does not infer state from a prior FSD command or a
transient central prompt.
