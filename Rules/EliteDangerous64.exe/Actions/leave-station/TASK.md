# Elite Dangerous supervised leave station

This version 7 interruptible linear Streaming Action begins only after a supervising
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
`ON`, the low-speed handover accepts either two strict `KNOWN` values from 0
through 10 within an eight-sample window, or four consecutive workflow-local
OCR observations of the same value from 0 through 10. The temporal path accepts
only `CONSTRAINED_CONFIDENCE_LOW` evidence where raw and constrained text match,
constrained confidence is at least `0.40`, and raw constraint margin is at most
`0.02`. A changed or non-qualifying candidate clears the temporal count.
Intervening `UNKNOWN` samples do not erase valid strict evidence, while a higher
reliable value does. It then calls
`elite-dangerous/set-throttle` with `100` and emits the resolved preset,
binding file, logical control, and physical key. After Mass Lock is `OFF` for
two consecutive samples, it calls the same Action with `0` and enters
`VERIFYING_STOP`. Completion requires three consecutive current-frame samples
whose unrestricted and digit-constrained candidates are both exactly `0`,
whose constrained confidence is at least `0.45`, and whose raw-versus-
constrained margin is at most `0.02`. The stop loop invokes only `ship-speed`;
it does not rerun flight-prompt or ship-status OCR after the already-confirmed
Mass Lock OFF gate, and its events mark those unobserved fields explicitly.
This temporal stop gate does not change the stricter single-frame `ship-speed`
threshold, so each contributing frame may remain honestly `UNKNOWN`. A
non-qualifying frame clears the count, and failure to confirm zero within 60
samples fails the workflow instead of reporting completion from the throttle
command alone.

Every update separates `commandedThrottle` from `observedSpeedState` and
`observedSpeedDisplayValue`. A successful input command is not reported as
observed speed. Events retain the speed classifier reason, raw text and
confidence, plus the Auto Launch age, movement latch, peak speed, strict and
temporal low-speed confirmation counts, temporal candidate text, selected
handover evidence mode, handover-candidate flag and explicit gate decision. Stop
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
if speed state and value contradict each other, if departure does not release
Mass Lock within 600 samples, or if zero speed is not visually confirmed within
60 samples after the 0% command. Cancellation stops the workflow and prevents
later controls. It never substitutes prior speed, commanded throttle, Player
Journal, or `Status.json` for missing visual evidence.
