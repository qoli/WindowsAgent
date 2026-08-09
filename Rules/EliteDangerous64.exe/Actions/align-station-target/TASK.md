# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

The Action first commands 0% throttle, then repeatedly reads
`elite-dangerous/compass` and issues one bounded yaw or pitch press
through `elite-dangerous/ship-attitude-control`. While the marker is hollow,
the Action locks a coarse yaw-left turn until it reaches the front hemisphere;
it deliberately does not reverse from the hollow marker's offset sign as that
projection crosses the compass antipode. Once solid, the dominant screen axis
selects yaw or pitch. Yaw retains the explicit 800 ms press at every distance;
pitch is distance-scaled to 800, 300, or 250 ms. After every ALIGN command the
Action waits 3.5 seconds before reading the next Gate. Live calibration on a
locked Station target showed that an 800 ms yaw press first appears to cross
the center, but settles after about 3.4 seconds with only roughly three
reference pixels of net movement. The previous fast feedback loop sampled that
transient and issued an opposite command, producing the observed oscillation.
Completion requires three consecutive solid samples inside the four-pixel
zone.

The optional `TRACK` mode is for moving targets such as a Nav Beacon. It does
not complete merely because the marker touches or remains briefly inside the
center zone. Instead it continues correcting for a bounded `trackingSamples`
window (120 samples by default), reports every ordinary observation and
command through the same stream, and returns center-contact plus maximum
consecutive-center counts. TRACK uses only the current Compass offset to choose
the next bounded correction. It does not calculate marker displacement,
distance delta, no-progress, or moving-away trends from consecutive frames:
the target's own motion makes those values invalid control-response evidence.
It emits those delta fields as null and never uses them as a Gate. ALIGN retains
the strict four-pixel center Gate plus stationary-target no-progress and
moving-away failures. Child-Action failures remain explicit in both modes.

Four consecutive binding-resolved Pitch commands with no Compass displacement
are a separately classified, reproduced Elite Dangerous input-initialization
state. The terminal error and the last update use
`ED_PITCH_INPUT_CONTEXT_NOT_READY` and include an `information` response telling
the supervising Agent to have the configured controller powered on or
reconnected, then retry without restarting the game. This avoids reopening
binding, scan-code, or Compass debugging for the known condition. Controller
enumeration is deliberately not a Gate: the controller that restored Pitch in
the reviewed live A/B test was not exposed by XInput.

In ALIGN, each post-command observation reports marker displacement,
center-distance delta, consecutive no-movement count, and a front-marker
moving-away trend. Four exactly stationary observations or five consecutive
front observations moving at least one reference pixel farther from center
fail explicitly rather than trusting one delayed frame or exhausting the full
command budget. TRACK does not produce or consume those inferred metrics.

Every observation and command is emitted as
`action.align-station-target.update`. Explicit `stream.activity` records expose
phase transitions and control pulses to the Windows Action OSD. The Action
fails rather than guessing when the compass target is absent, its hollow/solid
topology is ambiguous, a child Action fails, or the bounded command limit is
exhausted.
