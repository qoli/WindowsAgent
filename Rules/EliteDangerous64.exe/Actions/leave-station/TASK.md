# Elite Dangerous supervised leave station

This interruptible linear Streaming Action begins only after a supervising
model supplies `stationConfirmed: true`. It immediately starts observing raw
flight prompt text, classified flight status, and ship status, then emits
`AWAITING_AUTO_LAUNCH` with instructions for the model.

The model owns the deliberately slow Auto Launch menu interaction. It may take
screenshots and call `elite-dangerous/ui-control` one key at a time. The
workflow never blindly presses Select or navigates the station menu.

After observing `AUTO_LAUNCH` or `WAITING_IN_QUEUE`, the workflow requires that
the active prompt clear for three consecutive samples while Mass Lock remains
`ON`. It then calls `elite-dangerous/set-throttle` with `100` and emits the
resolved preset, binding file, logical control, and physical key. After Mass Lock
is `OFF` for two consecutive samples, it calls the same Action with `0`, emits
`COMPLETED`, and naturally terminates.

The fixed observation interval is 250 ms between sequential samples. The
workflow fails explicitly if Auto Launch is not observed within 600 samples,
if its prompt does not clear within 720 samples, if Mass Lock becomes `OFF`
before the 100% command, if Mass Lock remains `UNKNOWN` for 20 samples, or if
departure does not release Mass Lock within 600 samples. Cancellation stops
the workflow and prevents later controls.
