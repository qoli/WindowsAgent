# Elite Dangerous supervised leave station

This version 2 interruptible linear Streaming Action begins only after a supervising
model supplies `stationConfirmed: true`. It immediately starts observing raw
flight prompt text, classified flight status, ship status, and visual ship
speed, then emits `AWAITING_AUTO_LAUNCH` with instructions for the model.

The model owns the deliberately slow Auto Launch menu interaction. It may take
screenshots and call `elite-dangerous/ui-control` one key at a time. The
workflow never blindly presses Select or navigates the station menu.

After observing `AUTO_LAUNCH` or `WAITING_IN_QUEUE`, the workflow requires
three consecutive visual-handover samples: the raw prompt text is empty, Mass
Lock remains `ON`, and `elite-dangerous/ship-speed` returns a `KNOWN`, positive
HUD display value. Arbitrary unclassified prompt text and `UNKNOWN` speed reset
the handover count; neither can advance the workflow. It then calls
`elite-dangerous/set-throttle` with `100` and emits the resolved preset,
binding file, logical control, and physical key. After Mass Lock is `OFF` for
two consecutive samples, it calls the same Action with `0`, emits `COMPLETED`,
and naturally terminates.

Every update separates `commandedThrottle` from `observedSpeedState` and
`observedSpeedDisplayValue`. A successful input command is not reported as
observed speed. The terminal result likewise records the final command and the
last independent visual speed observation.

The fixed observation interval is 250 ms between sequential samples. The
workflow fails explicitly if Auto Launch is not observed within 600 samples,
if visual handover is not confirmed within 720 samples, if Mass Lock becomes
`OFF` before the 100% command, if Mass Lock remains `UNKNOWN` for 20 samples,
if speed state and value contradict each other, or if departure does not
release Mass Lock within 600 samples. Cancellation stops the workflow and
prevents later controls. It never substitutes prior speed, commanded throttle,
Player Journal, or `Status.json` for missing visual evidence.
