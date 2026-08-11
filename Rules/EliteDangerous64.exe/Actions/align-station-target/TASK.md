# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

By default the Action first commands 0% throttle, then repeatedly reads
`elite-dangerous/compass`. While the marker is hollow, the Action starts a
non-blocking `elite-dangerous/ship-attitude-hold` pitch-up lease and samples
Compass on a one-second start-to-start cadence until it reaches the front hemisphere;
it deliberately does not reverse from the hollow marker's offset sign as that
projection crosses the compass antipode. Live gravity-well evidence showed
that fixed Yaw can orbit a near-centered rear marker without changing its
hemisphere, while Pitch immediately moved the same marker toward SOLID. A
solid target more than 40 reference
pixels from center uses a leased hold. When both pitch and yaw components are
at least eight reference pixels, one compound lease overlaps both resolved
keys for diagonal movement; otherwise the dominant single axis is held.
The lease is renewed after each successful observation and released before an
axis change or entry into the 40-pixel fine band.

`controlProfile` defaults to `AUTO`. The Action reads the current
`filesystem/status` Flags and selects `SUPERCRUISE_ASSIST` while the game is in
Supercruise, otherwise `NORMAL_SPACE`. Missing, unavailable, or malformed
Status evidence fails explicitly; it never silently falls back to the
normal-space control law. A caller that already owns equivalent current state
may supply either explicit profile. The terminal output records both the
resolved profile and whether it came from input or Status.json.
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
from 120 ms to a bounded 240 ms in Supercruise, before the existing no-progress
Gate can terminate the run. Pulse observations keep the same one-second start-to-start cadence as
sustained control. Completion requires three consecutive solid samples at or
within 1.5 reference pixels. This is deliberately stricter than the Compass
Action's general-purpose four-pixel `centerZone`: live FSD testing proved that
the wider observation zone can still leave the game requesting target
alignment.

The `SUPERCRUISE_ASSIST` profile instead uses a sixteen-reference-pixel entry
radius and requires two current SOLID center contacts before handing off to the
required visible-target Action. A former one-frame completion was rejected by
live distance evidence: after thrust was restored, the target drifted from a
6px Compass contact through the rear hemisphere while distance increased from
2.71 to 3.04 kLs. It does not demand the normal-space three confirmations;
the following screen-space Action remains the owner of precise tracking. Live keyboard control showed
that an 80 ms Supercruise pulse can move the Compass marker roughly 11–17
reference pixels in some frames but repeatedly produced only 0–1 pixels during
live gravity-well alignment. The base pulse is therefore 120 ms, followed by
measured inertial drift from 3.6 to
9.5 and 12.4 pixels in one run, and from 12.4 through 19.4 to 26.9 pixels in a
rear-to-front run, so the normal-space Gate is unreachable without repeated
overshoot. When a sustained Supercruise turn is released inside this Gate, the
Action applies a 160 ms opposite-axis brake and then requires new Compass
observation before stable verification. The pre-brake SOLID frame cannot
complete the Action: live evidence showed that the former 300 ms release brake
could cross the compass antipode and leave the next frame HOLLOW even though
the sampled frame had been centered, while 80 ms under-braked the sustained
turn. A pulse
is braked only when it began outside the active 40-pixel Supercruise fine band.
Live evidence showed that braking an 80 ms pulse which began around 29–32
pixels produced a repeating 12–13 to 29–33 pixel oscillation; such fine-band
pulses now proceed directly to stable verification. If repeated ineffective
fine pulses invoke the bounded 240 ms recovery and that recovery crosses into
the Gate, it proceeds directly to the required second sample. Live testing
showed that the retired 80 ms recovery brake repeatedly moved a 14–16px result
back to 24–25px and recreated the recovery loop. Every remaining center-entry
brake invalidates the pre-command Compass sample and requires a
fresh post-brake observation before completion. Progress counters from a
released sustained lease are cleared before sizing the first bounded pulse;
otherwise old continuous-control evidence can incorrectly promote that pulse
to the 240 ms recovery strength. Live automatic-profile testing measured a
former 1000 ms recovery crossing the center by 34 reference pixels; 240 ms
retains a stronger recovery without repeating that overshoot.
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
consecutive-center counts. A HOLLOW marker uses a sustained pitch lease because
signed rear-projection offsets are not steering directions. If TRACK starts
with a rear marker it uses the proven `PITCH_UP` great-circle direction; if a
front marker crosses to HOLLOW, it instead reverses the last issued control.
The lease is released on the first SOLID observation, completion, failure,
cancellation, or expiry. TRACK does not apply the ALIGN-only release brake;
after HOLLOW returns to SOLID it observes one settling frame before choosing a
new correction. Ordinary front-marker pulses do not insert an extra cooldown:
the next one-Hertz Compass sample is fresh feedback and may immediately issue
the next bounded correction while it remains outside the hysteresis band. If
that post-command sample proves movement toward center and lands within the
24px exit boundary, TRACK preserves the gain for one observation and activates
the 20/24px hysteresis instead of stacking a second pulse. Live 160ms evidence
showed that `25.3px -> 21.6px` was already a useful correction; immediately
repeating it crossed the marker to HOLLOW and started a long recovery cycle.
Live gravity-well evidence also showed that a forced
cooldown reduced effective control to about 0.5 Hz: each 120 ms Pitch pulse
moved the target 1-5 reference pixels toward center, but the unused second let
it drift back and eventually cross to HOLLOW. The rear-to-front transition
settle remains local to the invocation and never reinterprets the HOLLOW offset
as a direction. A front marker otherwise uses only the current
Compass offset to choose the next bounded correction. In Supercruise, ALIGN
retains its calibrated 16-reference-pixel Gate while TRACK uses a separate
20-reference-pixel entry Gate with four pixels of hysteresis. Live
gravity-well evidence repeatedly reached 17-19px while the destination distance
was falling, but sharing ALIGN's stricter Gate caused unnecessary Pitch pulses
that eventually crossed the target to HOLLOW. The generic Compass four-pixel
zone is intentionally not used there: earlier approach evidence likewise showed
that correcting a valid 8-12px SOLID contact repeatedly drove it back to the
rear hemisphere and stalled the distance trend. Normal-space TRACK retains the
four-pixel Compass zone. Supercruise TRACK uses a two-stage bounded pulse:
160 ms from 30-40px and 120 ms from the 20px entry Gate through 30px. Live
testing found that 120 ms at a full one-Hertz command cadence without
hysteresis stalled around 21-24px, while 160 ms solved the far-band drift but
could cross a 27px marker directly to HOLLOW. The near-band 120 ms pulse is
therefore paired with the post-command settle and 20/24px hysteresis; it is not
the former unconditional 0.5 Hz loop. A 240 ms pulse followed by a sustained
HOLLOW lease crossed the front and rear hemispheres every second and remains
reserved only for measured no-progress recovery. This staged profile does not
weaken ALIGN's precision. TRACK measures post-pulse Compass movement and promotes
an ineffective pulse to 240 ms only after two stationary observations. A front
marker above 40px uses a bounded coarse pulse; inside 20px it only observes. TRACK does
not use distance delta or moving-away trends from consecutive frames: the
target's own motion makes those values invalid directional evidence. It does
measure immediate post-command marker displacement solely to size bounded
recovery pulses; it does not use that value as a TRACK failure Gate. ALIGN retains
the strict 1.5-pixel alignment Gate plus stationary-target no-progress and
moving-away failures. Child-Action failures remain explicit in both modes.

`TRACK` also accepts `targetMotion`. The default `MOVING` preserves the
20/24px following Gate described above. `STATIC` is for a selected planet,
station, or system destination whose direction must remain precise while the
ship advances. In Supercruise it uses a 4px entry Gate, 6px hysteresis exit,
and an 80 ms micro-pulse inside 6px; the 6-30px and 30-40px bands use 160 ms.
Its cadence is 650 ms (about 1.5 Hz with the
measured 0.58 s Compass capture), because live evidence showed a precisely
centered static marker drifting from 5-9px back to 15-18px between one-second
samples. STATIC never promotes a pulse inside 30px to the moving-target 240 ms
no-progress recovery; repeated live crossings began with that promotion near
23.8px. A later 120ms run remained stable but equilibrated at 14-23px while
the destination passed at 567 Ls; the bounded 160ms mid-band reduced the next
miss to 308 Ls but still oscillated between 8-16px. The 4/6px Gate therefore
keeps full gain until the collision-course error is materially smaller. Live
4/6px evidence then reached 0-4px repeatedly and produced the best 195 Ls miss.
An 80ms center feed-forward experiment did not improve that result (196 Ls),
while applying 160ms inside the fine Gate regressed to 259 Ls; both experiments
were rejected. The remaining error is an observation-cadence problem rather
than justification for more open-loop gain. STATIC
also latches the front projection established by its required
preceding ALIGN for every later HOLLOW classification. A static
destination cannot physically move behind the ship after these bounded pulses;
live evidence instead showed a one-frame HOLLOW classification at 22px with
continuous screen geometry. Treating that frame as a rear-antipode transition
started sustained control and caused the actual miss. The event retains the
observed HOLLOW value and reports `STATIC_TRACK_PRESENTATION_CONTINUITY`, while
control stays on the dominant continuous screen axis and remains bounded to
80/160ms. STATIC never enters the rear sustained lease or an 800ms coarse
pulse after ALIGN; MOVING TRACK keeps the ordinary explicit rear-recovery
behavior. This
distinction is required for goal
semantics: the moving-target tolerance can follow a Nav Beacon but repeatedly
missed a planet by roughly 1.2-1.6 kLs when reused as a collision-course Gate.
The selected motion profile is emitted when the streaming Action starts and is
returned in the final output.

TRACK requires its first detected marker to be SOLID. An initial HOLLOW marker
fails before sending attitude input because the invocation has no prior control
direction to reverse safely. The caller must run ALIGN immediately before
TRACK; once TRACK has issued a front-marker command, later SOLID-to-HOLLOW
transitions have the control history needed for deterministic recovery.

Four consecutive binding-resolved Pitch commands with no Compass displacement
are a separately classified, reproduced Elite Dangerous input-initialization
state. The terminal error and the last update use
`ED_PITCH_INPUT_CONTEXT_NOT_READY` and include an `information` response telling
the supervising Agent to have the configured controller powered on or
reconnected, then retry without restarting the game. This avoids reopening
binding, scan-code, or Compass debugging for the known condition. Controller
enumeration is deliberately not a Gate: the controller that restored Pitch in
the reviewed live A/B test was not exposed by XInput.

Each observation reports its start time, execution duration, and
start-to-start interval together with `NONE`, `SUSTAINED`, or `PULSE` control
mode and current lease evidence. Each post-command observation also reports
marker displacement and consecutive no-movement count. ALIGN additionally
reports center-distance delta plus a front-marker moving-away trend. Four
exactly stationary observations or five consecutive front observations moving
at least one reference pixel farther from center fail explicitly in ALIGN
rather than trusting one delayed frame or exhausting the full command budget.
TRACK consumes only its displacement count for bounded pulse sizing.

Every observation and command is emitted as
`action.align-station-target.update`. Explicit `stream.activity` records expose
phase transitions and control pulses to the Windows Action OSD. The Action
fails rather than guessing when the compass target is absent, its hollow/solid
topology is ambiguous, a child Action fails, or the bounded command limit is
exhausted. Failure or cancellation runs the registered lease STOP compensation;
the 2500 ms runtime lease independently expires if the workflow cannot renew it.
