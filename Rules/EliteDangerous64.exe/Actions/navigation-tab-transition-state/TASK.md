# Observe the transient Navigation-list frame

This internal finite observation reads only the calibrated 4x4 pixel sample
beside the `NAVIGATION` tab label. It exists for transitions where Elite
Dangerous renders the Navigation list for about one second between a detail
card and the forward cockpit view. The ordinary four-tab observation performs
four sequential region captures and can finish after that transient frame has
already disappeared.

`NAVIGATION` means the current sample independently confirmed the selected
Navigation-tab colour. `NOT_CONFIRMED` deliberately does not distinguish a
detail card, another panel state, or the forward view and cannot prove that a
panel is closed. Callers must use a separate full panel postcondition after the
transition is captured.
