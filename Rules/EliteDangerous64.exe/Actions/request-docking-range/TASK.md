# Elite Dangerous request docking range

This finite composite Action captures the current lower-left Target distance,
runs the resident PP-OCR recognizer, and returns `ALLOWED`, `DENIED`, or
evidence-preserving `UNKNOWN` for the strict displayed-distance Gate
`distanceMeters < 7500`.

Run it before opening the Target panel, while the cockpit is in its settled
forward view. `ALLOWED` means only that the current visible distance passes the
range Gate; it does not prove that the correct Starport is selected, that
`REQUEST DOCKING` is focused, or that a docking request was accepted. The
Action does not register a Monitor and never invokes a control Action.
