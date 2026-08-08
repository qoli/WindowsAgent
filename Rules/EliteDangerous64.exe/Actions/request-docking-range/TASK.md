# Elite Dangerous request docking range

This finite composite Action scans the reviewed lower-left horizontal HUD band
with the resident PP-OCR text-regions pipeline, selects exactly one current
distance region, and returns `ALLOWED`, `DENIED`, or evidence-preserving
`UNKNOWN` for the strict displayed-distance Gate `distanceMeters < 7500`.

Run it before opening the Target panel, while the cockpit is in its settled
forward view. `ALLOWED` means only that the current visible distance passes the
range Gate; it does not prove that the correct Starport is selected, that
`REQUEST DOCKING` is focused, or that a docking request was accepted. The
Action does not register a Monitor and never invokes a control Action.
It does not use ScreenParser or fall back to the retired fixed distance ROI.
