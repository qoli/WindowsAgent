# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

`alignmentPurpose=CENTER` preserves the standalone Compass-centering contract.
`alignmentPurpose=VISIBLE_HANDOFF` is a narrower Supercruise STATIC ALIGN
contract for an owning workflow that immediately delegates final geometry to
`align-visible-target`: it requires two current SOLID samples after entering
the four-reference-pixel Compass center zone, with a six-pixel verification
exit, and does
not issue a center-entry counter-pulse. It only proves that the rear/coarse
Compass phase has placed the destination in the visible-target controller's
domain; it is not precise alignment and is rejected for TRACK, MOVING targets,
or a non-Supercruise control profile.

Live Houssay Ring evidence bounded that coarse domain: a 19.105-pixel Compass
contact satisfied the former 16+4 Gate while the destination label remained at
the extreme left edge of the forward HUD. A later Supercruise Assist run
reproduced a 15-to-23-pixel cycle while the visible-target detector still had
no usable proposal. `VISIBLE_HANDOFF` now shares the proven 300 ms inner pulse
through 24 pixels and requires the true four/six-pixel center Gate before it
delegates local reticle geometry.

`alignmentPurpose=HYPERSPACE_CHARGE` is the pre-FSD Compass-to-visible-target
handoff. It
is valid for STATIC ALIGN in normal space or Supercruise; the start mode must
select the matching control profile. It requires three consecutive SOLID
Compass observations after entering ten reference pixels in normal space, or
the actual four-pixel Compass center zone in Supercruise, with a two-pixel
verification hysteresis. A 45-second normal-space Evidence interval with zero
gaps or missing frames showed the 40 ms control law repeatedly cycling through
roughly 5-15 pixels until the full 120-command budget was exhausted, even
though the destination reticle remained visible to the next controller. The
ten/twelve-pixel Gate stops injecting at that useful handoff instead of asking
Compass to perform the following controller's job. A later 81-second
Supercruise Evidence interval with 81 frames, zero gaps, and zero missing slots
showed the former 40 ms inner-band pulses being lost to heading drift and
trapping the Compass at roughly 15-21 pixels. Replacing them with 160 ms
pulses reached a second 9-13-pixel band, but a following 24-frame, zero-gap
Evidence interval proved that handing off at 19.647 pixels left both the target
label and its three-quarter focus frame outside the visible-target pipeline's
valid ROI, and a direct visible-target check after a ten/twelve-pixel completion
still returned `TARGET_TEXT_NOT_FOUND`. A bounded 300 ms diagnostic Yaw pulse
moved the same current target from 13 pixels to 5 pixels. Supercruise therefore
uses that effective inner-band pulse and must enter the true four-pixel Compass
center zone, then remain within six pixels for three current observations,
before handoff. Normal space retains its calibrated 40 ms pulse and
ten/twelve-pixel Gate. The caller must still require exact visible-target completion and recheck the
stellar `safeToCharge` Gate before sending FSD input.

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
band. The first sample that crosses into the four-pixel alignment radius after a
pulse applies a 100 ms opposite-axis brake before stable verification. When two consecutive pulse-band observations show no measurable
response, the next pulse is raised from 120 ms to 400 ms in normal space, or
from 120 ms to a bounded 240 ms in Supercruise, before the existing no-progress
Gate can terminate the run. Pulse observations keep the same one-second start-to-start cadence as
sustained control. Ordinary normal-space completion requires three consecutive
SOLID samples within the Compass Action's calibrated four-pixel `centerZone`.

`targetMotion=STATIC` also affects normal-space ALIGN. This combination is the
Station/Supercruise-entry handoff: live testing showed the selected Station
marker repeatedly settling between roughly 8 and 13 reference pixels while
120 ms pulses continued to perturb the already useful direction. Later live
planet-target testing reproduced the same quantized equilibrium at 14–19
pixels: a 120 ms pitch pulse alternated indefinitely between the two sides of
an already FSD-usable heading. It enters a 16-pixel Gate and then uses four
pixels of verification hysteresis, still
requiring three current SOLID observations. When a 300 ms medium-band pulse
first enters that Gate, it applies the existing 100 ms opposite-axis brake,
discards the pre-brake contact, and requires three fresh SOLID observations.
Live 1 FPS Evidence showed the previous unbraked law repeatedly entering at
roughly 9–11 pixels, retaining angular velocity, and drifting back to 16–19
pixels; it needed 60 samples and 43 commands before three contacts happened to
survive. Near-band 120 ms pulses do not trigger this brake. System-jump precision alignment
does not request `STATIC` and retains the four-pixel Gate. This is a declared
control law, not a fallback to OCR or a visible-target search.

The `SUPERCRUISE_ASSIST` profile with the default `MOVING` target instead uses
a sixteen-reference-pixel entry radius and requires two current SOLID center
contacts before returning control to its owning workflow. `STATIC` ALIGN uses
an eight-pixel entry and ten-pixel verification hysteresis, with 40 ms
ultra-fine pulses inside twelve pixels, 80 ms fine pulses after a measured
ultra-fine no-progress escalation, and 160 ms pulses through thirty pixels. This accepts the
observed 4.472-5.831-pixel integer-marker equilibria without relaxing STATIC
TRACK's four-pixel entry and six-pixel hysteresis. The owning Supercruise
Assist workflow now performs its collision-course refinement from the visible
destination after the game emits `FSD_ALIGNMENT_REQUIRED`, so this Compass
Gate is deliberately only the front/coarse handoff.
When two consecutive 40 ms `STATIC` ALIGN pulses inside twelve pixels produce no
measurable Compass displacement, the next pulse is promoted only to the
existing bounded 160 ms mid-band duration. A 40 ms or 80 ms pulse that enters
the eight-pixel Gate is not immediately counter-pulsed; only a 160 ms entry
receives the bounded 80 ms center-entry brake. This handles quantized one-pixel
Compass sampling without invoking the rejected 240 ms recovery. `STATIC` TRACK
retains its fixed 80/160 ms law and stricter four-pixel completion Gate.
Live Station Assist evidence showed that the former 16/20px ALIGN Gate repeatedly
returned completed at 9.8-16.7px while the game continued to display `ALIGN WITH
TARGET DESTINATION`; the owning workflow then re-entered the same ineffective
alignment loop. The stricter STATIC law keeps Compass as the feedback source
until the game's required collision-course accuracy is materially satisfied.
The ten-pixel fine-control handoff is not a wider completion Gate: a deployed
four-pixel test showed 160 ms corrections repeatedly crossing between roughly
5 and 10 pixels without entering the required center, so only the bounded pulse
size changes before the same four-pixel verification. Once a bounded STATIC
Supercruise pulse first enters four pixels, the Action applies one 80 ms
opposite-axis brake and discards that pre-brake sample. Live evidence reached
3.162 pixels but drifted to 15.133 pixels on the following frame without this
brake; completion therefore still requires two fresh post-brake observations.
A former one-frame completion was rejected by
live distance evidence: after thrust was restored, the target drifted from a
6px Compass contact through the rear hemisphere while distance increased from
2.71 to 3.04 kLs. It does not demand the normal-space three confirmations.
Consumers that need continued Station alignment can call this Action again or
use its STATIC TRACK mode; this Action does not substitute an OCR/visible-target
search. Live keyboard control showed
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
Normal-space STATIC alignment now treats the 16–32-pixel pre-Gate band as
single-axis fine control with an 80 ms pulse and does not apply the
medium-pulse brake there. Live Evidence first showed the retired 300 ms pulse
plus 100 ms brake cycling from 10–12 pixels back to 16–18 pixels for 56
commands. After removing that brake, a two-axis target still cycled between
17–25 pixels because both near-center axes received the same 120 ms pulse and
the 25-pixel sample re-entered the 300 ms path. The current policy converges
only the dominant component in this band, without changing diagonal control
for larger entries or other profiles. Larger normal-space entries retain the
bounded 100 ms brake, and every post-brake completion still requires fresh
observations. This
profile remains limited to front-hemisphere coarse hyperspace/Supercruise
alignment. A live 32-pixel experiment was
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

Both modes accept `targetMotion`; the detailed tracking behavior below applies
when `mode=TRACK`. The default `MOVING` preserves the
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
