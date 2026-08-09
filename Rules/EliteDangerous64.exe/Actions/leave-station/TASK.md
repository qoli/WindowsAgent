# Elite Dangerous supervised leave station

This version 8 interruptible linear Streaming Action begins only after a supervising
model supplies `stationConfirmed: true`. It immediately starts observing raw
flight prompt text, classified flight status, ship status, and visual ship
speed, then emits `AWAITING_AUTO_LAUNCH` with instructions for the model.

The model owns the deliberately slow Auto Launch menu interaction. It may take
screenshots and call `elite-dangerous/ui-control` one key at a time. The
workflow never blindly presses Select or navigates the station menu.

After observing `AUTO_LAUNCH` or `WAITING_IN_QUEUE`, the workflow latches that
positive evidence and follows the visible motion lifecycle. A `MOVING` speed
observation, representing a qualified value of at least 10, proves that Auto
Launch moved the ship. Five later samples without
another classified Auto Launch prompt establish a handover candidate; raw OCR
garbage is neutral rather than a false clear signal. While Mass Lock remains
`ON`, the low-speed handover accepts two classified `STOPPED` or `LOW_SPEED`
observations within an eight-sample window. `UNKNOWN` never contributes to a
Gate. Intervening `UNKNOWN` samples do not erase valid low-speed evidence,
while a later `MOVING` observation does. It then calls
`elite-dangerous/set-throttle` with `100` and emits the resolved preset,
binding file, logical control, and physical key. After Mass Lock is `OFF` for
two consecutive samples, it calls the same Action with `0` and enters
`VERIFYING_STOP`. Completion requires three consecutive current-frame
`STOPPED` samples backed by the dedicated slashed-zero pixel topology in
`ship-speed`. A qualified multi-digit OCR observation makes that classifier
conflicting rather than stopped. The stop loop invokes only `ship-speed`;
it does not rerun flight-prompt or ship-status OCR after the already-confirmed
Mass Lock OFF gate, and its events mark those unobserved fields explicitly.
Each contributing frame must be classified `STOPPED`; `LOW_SPEED`, `MOVING`,
or `UNKNOWN` clears the count. Failure to confirm zero within 60 samples fails
the workflow instead of reporting completion from the throttle command alone.

Every update separates `commandedThrottle` from `observedSpeedState` and
`observedSpeedDisplayValue`. A successful input command is not reported as
observed speed. Events retain the speed classifier state, diagnostic raw
candidate, reason, raw text and confidence, plus the Auto Launch age, movement
latch, peak speed, classified low-speed confirmation count, selected handover
evidence mode, handover-candidate flag and explicit gate decision. Stop
verification also reports its age, zero confirmation count, and decision. The
terminal result records the final command and the evidence from the final
independent visual speed observation.

The fixed observation interval is 250 ms between sequential samples. The
workflow skips at most five explicitly coded transient WGC capture failures,
emitting every skipped sample as `OBSERVATION_ERROR`; a sixth such failure or
any non-WGC child failure terminates explicitly. Before commanding 100%, it
registers a runtime failure compensation that unconditionally invokes throttle
0 if any later failure occurs. A successful normal-path throttle-0 command
clears the compensation.

The workflow fails explicitly if Auto Launch is not observed within 600 samples,
if lifecycle handover is not confirmed within 720 samples, if Mass Lock becomes
`OFF` before the 100% command, if Mass Lock remains `UNKNOWN` for 20 samples,
if speed state and values contradict each other, if throttle 100 does not
produce a `MOVING` observation within 20 successful samples, if departure does not release
Mass Lock within 600 samples, or if zero speed is not visually confirmed within
60 samples after the 0% command. Cancellation stops the workflow and prevents
later controls. It never substitutes prior speed, commanded throttle, Player
Journal, or `Status.json` for missing visual evidence.
