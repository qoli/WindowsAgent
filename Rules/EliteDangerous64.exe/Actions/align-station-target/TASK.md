# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

By default the Action first commands 0% throttle, then repeatedly reads
`elite-dangerous/compass`. While the marker is hollow, the Action starts a
non-blocking `elite-dangerous/ship-attitude-hold` yaw-left lease and samples
Compass on a one-second start-to-start cadence until it reaches the front hemisphere;
it deliberately does not reverse from the hollow marker's offset sign as that
projection crosses the compass antipode. A solid target more than 40 reference
pixels from center uses a leased hold. When both pitch and yaw components are
at least eight reference pixels, one compound lease overlaps both resolved
keys for diagonal movement; otherwise the dominant single axis is held.
The lease is renewed after each successful observation and released before an
axis change or entry into the 40-pixel fine band.
After at least one target observation, the Action also treats at most two
consecutive missing-marker frames as a transient compass-edge crossing. It
releases the active lease and resumes observation; an initially missing target
or a third consecutive missing frame still fails as missing destination-lock
evidence.

Inside 40 pixels, control returns to bounded pulses through
`elite-dangerous/ship-attitude-control`. Both Yaw and Pitch use 300 ms pulses
inside the 40-pixel band and 120 ms pulses inside the 16-pixel near-center
band. The first sample that crosses into the 1.5-pixel alignment radius after a
pulse applies a 100 ms opposite-axis brake before stable verification. When two consecutive pulse-band observations show no measurable
response, the next pulse is raised from 120 ms to 400 ms in normal space, or
from 80 ms to a bounded 1000 ms in Supercruise, before the existing no-progress
Gate can terminate the run. Pulse observations keep the same one-second start-to-start cadence as
sustained control. Completion requires three consecutive solid samples at or
within 1.5 reference pixels. This is deliberately stricter than the Compass
Action's general-purpose four-pixel `centerZone`: live FSD testing proved that
the wider observation zone can still leave the game requesting target
alignment.

The `SUPERCRUISE_ASSIST` profile instead uses a sixteen-reference-pixel entry
radius and accepts one current SOLID center contact before handing off to the
required visible-target Action. It does not demand three one-second Compass
confirmations: live Supercruise inertia repeatedly carried a correctly centered
marker back outside the coarse Gate before that cadence, while the following
screen-space Action is the owner of precise stable alignment. Live keyboard control showed
that the minimum effective 80 ms Supercruise pulse moves the Compass marker
roughly 11–17 reference pixels, followed by measured inertial drift from 3.6 to
9.5 and 12.4 pixels in one run, and from 12.4 through 19.4 to 26.9 pixels in a
rear-to-front run, so the normal-space Gate is unreachable without repeated
overshoot. When a sustained Supercruise turn is released inside this Gate, the
Action applies an 80 ms opposite-axis brake and then requires a new Compass
observation before stable verification. The pre-brake SOLID frame cannot
complete the Action: live evidence showed that the former 300 ms release brake
could cross the compass antipode and leave the next frame HOLLOW even though
the sampled frame had been centered. A pulse
is braked only when it began outside the active 40-pixel Supercruise fine band.
Live evidence showed that braking an 80 ms pulse which began around 29–32
pixels produced a repeating 12–13 to 29–33 pixel oscillation; such fine-band
pulses now proceed directly to stable verification. If repeated ineffective
fine pulses invoke the bounded 1000 ms recovery and that recovery crosses into
the Gate, an 80 ms reverse brake counters its measured residual inertia. Every
center-entry brake invalidates the pre-command Compass sample and requires a
fresh post-brake observation before completion. Progress counters from a
released sustained lease are cleared before sizing the first bounded pulse;
otherwise old continuous-control evidence can incorrectly promote that pulse
to the 1000 ms recovery strength.
Normal-space entry braking retains its own 16-pixel fine-band boundary and
100 ms duration. This
profile remains limited to front-hemisphere coarse hyperspace/Supercruise
alignment; the following visible-target Action still owns precise on-screen
destination alignment before charging or travel. A live 32-pixel experiment was
rejected because a 28.8-pixel Compass result still left the target label outside
the central OCR field.

`stopBeforeAlign=false` is reserved for an owning flight workflow that already
controls throttle, such as an active Supercruise approach. In that mode this
Action keeps the same visual alignment and verification contract but does not
mutate throttle on entry.

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
the strict 1.5-pixel alignment Gate plus stationary-target no-progress and
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

In ALIGN, each observation reports its start time, execution duration, and
start-to-start interval together with `NONE`, `SUSTAINED`, or `PULSE` control
mode and current lease evidence. Each post-command observation also reports marker displacement,
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
exhausted. Failure or cancellation runs the registered lease STOP compensation;
the 2500 ms runtime lease independently expires if the workflow cannot renew it.
