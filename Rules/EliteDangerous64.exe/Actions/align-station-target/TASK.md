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
lateral correction and pitch/yaw handle fine correction. Rear, coarse, and
fine phases use explicit 800, 800, and 400 ms holds respectively. Completion
requires three consecutive solid samples inside the four-pixel center zone.

The optional `TRACK` mode is for moving targets such as a Nav Beacon. It does
not complete merely because the marker touches or remains briefly inside the
center zone. Instead it continues correcting for a bounded `trackingSamples`
window (120 samples by default), reports every ordinary observation and
command through the same stream, and returns center-contact plus maximum
consecutive-center counts. A front-marker moving-away trend remains observable
but is not a failure in this mode because target motion can create it. The
no-movement and child-Action failure Gates remain active.

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
