# Precisely align a visible Elite Dangerous HUD target

This interruptible linear Streaming Action refines a target that is already
visible in the forward HUD. It takes the selected target name, repeatedly calls
`elite-dangerous/supercruise-target-position`, and applies bounded dominant-axis
Pitch or Yaw pulses until the OCR-derived target marker remains within 12
reference pixels of the 1920x1080 screen centre for three consecutive samples.

This is deliberately separate from `align-station-target`. Compass alignment
handles rear-hemisphere and large-angle navigation; visible-target alignment
handles the tighter on-screen Gate needed by FSD and similar forward-target
workflows. The Action does not select a target or engage FSD. By default it
commands 0% throttle before turning. Unknown target text is tolerated for at
most seven consecutive frames because live Pitch/Yaw motion can temporarily
blur or animate the OCR label; no control input is sent from an UNKNOWN frame,
and the eighth consecutive miss fails explicitly. Only exact deadline failures
receive five bounded retries; other observation failures remain explicit.

`positionSource=DESTINATION` preserves the selected-destination OCR path.
`positionSource=ESCAPE_VECTOR` instead calls the dedicated
`escape-vector-visible-position` Gate. The latter must actually detect the
two-line blue reticle label; a SOLID Compass marker alone never selects it.

`searchWhenUnknown=true` is reserved for an owning destination workflow that
has already verified an exact Navigation destination lock. If the requested
name is outside the OCR bands, the Action performs a bounded yaw sweep: four
600 ms left pulses followed by at most eight right pulses. Every step retains
the local heat Gate and reruns exact-name OCR. If a search pulse displaces the
heat digits enough to return UNKNOWN, the Action settles for 500 ms and retries
that visual heat observation; three consecutive settled UNKNOWN results still
fail closed. It never substitutes another visible label; failure to recover the
requested name remains explicit. Escape Vector alignment never enables this
search mode.

The Escape Vector profile is time-sensitive: it samples at 350 ms cadence,
uses 500/300/160 ms correction pulses above 40/20/12 pixels, and accepts two
consecutive within-12-pixel frames. The ordinary destination profile retains
its 750 ms cadence, 300/160/80 ms gains, and three confirmations.

Every alignment cycle first calls the visual `ship-heat` Action. Known heat at
or above 75% fails immediately; three consecutive UNKNOWN heat observations
also fail. Events carry `heatState` and `heatPercent`. A charging parent must
register FSD-cancel and 0%-throttle failure compensation before invoking this
Action, so a heat failure closes the charge instead of merely stopping OCR.

`heatPolicy=ESCAPE_VECTOR_CHARGE` is a narrow exception available only with
`positionSource=ESCAPE_VECTOR`. FSD charge animation can temporarily hide the
heat digits even while the blue Escape Vector remains measurable. After a
known reading no higher than 60%, this policy tolerates UNKNOWN heat for at
most four seconds and emits `HEAT_UNKNOWN_ESCAPE_CHARGE_GRACE`; known heat at
75% still fails immediately. The grace expires without renewal and does not
apply to destination alignment or visible-target search.
