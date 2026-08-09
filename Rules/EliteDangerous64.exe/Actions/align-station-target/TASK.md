# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

The Action first commands 0% throttle, then repeatedly reads
`elite-dangerous/compass` and issues one bounded roll, pitch, or fine yaw press
through `elite-dangerous/ship-attitude-control`. While the marker is hollow,
the Action locks a coarse pitch-up turn until it reaches the front hemisphere;
it deliberately does not reverse from the hollow marker's offset sign as that
projection crosses the compass antipode. Once solid, roll handles coarse
lateral correction and pitch/yaw handle fine correction. Rear markers retain
the explicit 800 ms turn. Solid-marker correction is distance-scaled: over 40
reference pixels uses 800 ms, 17 through 40 pixels uses 300 ms, and the final
16 pixels uses 250 ms. These bands prevent the reviewed failure where an 800 ms
pulse at 14 pixels crossed the center and produced a roughly 30-pixel
oscillation. Live calibration also showed that 120 ms stalled outside the
center Gate, while 250 ms moved a five-pixel offset into the strict four-pixel
zone. Completion requires three consecutive solid samples inside that zone.

The optional `TRACK` mode is for moving targets such as a Nav Beacon. It does
not complete merely because the marker touches or remains briefly inside the
center zone. Instead it continues correcting for a bounded `trackingSamples`
window (120 samples by default), reports every ordinary observation and
command through the same stream, and returns center-contact plus maximum
consecutive-center counts. A front-marker moving-away trend remains observable
but is not a failure in this mode because target motion can create it. If the
marker is already inside the 16-pixel fine band and repeated fine pulses stop
producing measurable movement, TRACK holds that near-center solution without
more pulses and resumes correction if the marker drifts outside the band.
ALIGN retains the strict four-pixel center Gate. Child-Action failures and
no-movement outside the fine band remain active failures.

Each post-command observation reports marker displacement, center-distance
delta, consecutive no-movement count, and a front-marker moving-away trend.
Four exactly stationary observations or five consecutive front observations
moving at least one reference pixel farther from center fail explicitly rather
than trusting one delayed frame or exhausting the full command budget.

Every observation and command is emitted as
`action.align-station-target.update`. Explicit `stream.activity` records expose
phase transitions and control pulses to the Windows Action OSD. The Action
fails rather than guessing when the compass target is absent, its hollow/solid
topology is ambiguous, a child Action fails, or the bounded command limit is
exhausted.
