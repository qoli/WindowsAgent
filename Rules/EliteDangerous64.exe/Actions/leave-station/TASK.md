# Elite Dangerous supervised leave station

This version 3 interruptible linear Streaming Action begins only after a supervising
model supplies `stationConfirmed: true`. It immediately starts observing raw
flight prompt text, classified flight status, ship status, and visual ship
speed, then emits `AWAITING_AUTO_LAUNCH` with instructions for the model.

The model owns the deliberately slow Auto Launch menu interaction. It may take
screenshots and call `elite-dangerous/ui-control` one key at a time. The
workflow never blindly presses Select or navigates the station menu.

After observing `AUTO_LAUNCH` or `WAITING_IN_QUEUE`, the workflow latches that
positive evidence and follows the visible motion lifecycle. A reliable speed of
at least 15 proves that Auto Launch moved the ship. Five later samples without
another classified Auto Launch prompt establish a handover candidate; raw OCR
garbage is neutral rather than a false clear signal. While Mass Lock remains
`ON`, two reliable values from 0 through 10 within an eight-sample window prove
the low-speed handover. Intervening `UNKNOWN` speed samples do not erase valid
evidence, while a higher reliable value does. It then calls
`elite-dangerous/set-throttle` with `100` and emits the resolved preset,
binding file, logical control, and physical key. After Mass Lock is `OFF` for
two consecutive samples, it calls the same Action with `0`, emits `COMPLETED`,
and naturally terminates.

Every update separates `commandedThrottle` from `observedSpeedState` and
`observedSpeedDisplayValue`. A successful input command is not reported as
observed speed. Events retain the speed classifier reason, raw text and
confidence, plus the Auto Launch age, movement latch, peak speed, low-speed
confirmation count, handover-candidate flag and explicit gate decision. The
terminal result likewise records the final command and the last independent
visual speed observation.

The fixed observation interval is 250 ms between sequential samples. The
workflow fails explicitly if Auto Launch is not observed within 600 samples,
if lifecycle handover is not confirmed within 720 samples, if Mass Lock becomes
`OFF` before the 100% command, if Mass Lock remains `UNKNOWN` for 20 samples,
if speed state and value contradict each other, or if departure does not
release Mass Lock within 600 samples. Cancellation stops the workflow and
prevents later controls. It never substitutes prior speed, commanded throttle,
Player Journal, or `Status.json` for missing visual evidence.
