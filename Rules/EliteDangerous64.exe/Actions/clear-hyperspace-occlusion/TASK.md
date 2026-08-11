# Clear a gravity obstruction with short-charge Compass probes

This interruptible linear Streaming Action owns the time-sensitive segment
that escapes a local gravity well into Supercruise. Its caller still owns
restoring and aligning any later hyperspace destination.

The Action commands 0% throttle and records `hyperspace-target-occlusion` only
as diagnostic context. Forward-view CV does not choose an attitude command or
claim an escape angle. Landing Gear and Cargo Scoop must be visually OFF. The
latest AVAILABLE `Status.json` must describe normal-space idle flight with
Mass Lock OFF, and three visual heat readings at or below 60% are required
before a probe.

## Ephemeral Escape Vector state

The Supercruise Escape Vector exists only while FSD is charging. It is not a
durable target, Monitor result, or state that remains visible after charge is
cancelled. Each prealignment cycle therefore has a strict lifetime:

1. keep throttle at 0% and briefly start Supercruise charging;
2. require a newer charging Status snapshot;
3. collect up to sixteen Compass samples at 137 ms cadence, but stop as soon
   as two consistent SOLID or HOLLOW observations are available;
4. require at least two consistent SOLID or HOLLOW observations;
5. cancel charge and verify a newer non-charging Status snapshot;
6. consume that Compass snapshot with at most one bounded attitude pulse;
7. invalidate the snapshot and cool before another probe.

Streaming events expose that lifetime as `escapeVectorEvidenceState`:
`LIVE_CHARGE` is a currently visible charging Compass observation,
`CACHED_ONE_SHOT` is Action-local evidence that may authorize only the next
attitude pulse, and `EXPIRED` means no later command may use it. Historical
Compass coordinates in the log are evidence, not a durable world-state claim.

An absent-to-detected Compass transition, presentation change, or at least
eight pixels of pre/post-charge movement establishes charge ownership. Missing
samples are emitted explicitly instead of silently shrinking the voting
window. The 137 ms interval intentionally walks across the flashing Compass
phase; a fixed 100 ms interval reproduced alternating and then fully
phase-locked `COMPASS_NOT_VISIBLE` bursts. Sixteen samples also cover the
observed delayed HUD presentation after a later probe charge.

A HOLLOW marker is an antipodal rear projection. Its small signed offset is
not treated as a reliable screen-space angle. The Action reuses the proven
topology rule: it applies a fixed 1000 ms `PITCH_UP` pulse on the ship's faster
primary turning axis until a later short-charge probe becomes SOLID. Turning
uses an interruptible key lease rather than a one-second finite key cap. A
HOLLOW snapshot authorizes one 3000 ms segment. A SOLID marker uses its current
offset with 3000/1800/700/200 ms segments at greater-than 40/16/6/4 pixel
distance bands. A worsening fresh SOLID distance reverses the prior direction.
Up to eight probe/segment cycles are bounded by the local heat gates. Every
segment consumes exactly one snapshot; the next segment requires a new probe.

A SOLID Compass observation is only a front-hemisphere Gate. While the probe
charge remains active, the Action calls `escape-vector-visible-position`.
`UNKNOWN` means the reticle is still outside or unresolved in the forward OCR
ROI, so the charge is cancelled and one Compass-derived segment is allowed.
Only `DETECTED` hands control to heat-protected `align-visible-target` with
`positionSource=ESCAPE_VECTOR`. Successful visible alignment keeps the same
safe Supercruise charge alive for entry; SOLID alone never enters this path.

Once a probe reports a centered SOLID marker, the probe charge is still
cancelled. Formal entry then starts a new Supercruise charge and commands 100%
throttle. The Action discards the prealignment snapshot and requires fresh
charge-owned Compass evidence before accepting alignment or Supercruise
entry. It may make current-charge corrections while that Compass remains
visible, but it never treats a cancelled-probe observation as a completion
Gate.

## Safety and completion

Known heat at or above 70% cancels a prealignment probe; formal charge cancels
at 75% until visible Escape Vector alignment completes. After that handoff, the
game's FSD entry countdown may raise a heat wall that hides the reticle. The
Action therefore stops requiring reticle evidence, tolerates the generic
Status Over Heating flag, and uses a phase-local 160% visual heat ceiling while
waiting for Supercruise. Heat OCR may become UNKNOWN behind that transient heat
wall, so UNKNOWN is logged but does not cancel during the already-aligned,
bounded countdown. Every heat OCR return is followed by a fresh fast Status
read; a confirmed Supercruise transition wins the race before any cancellation.
Mass Lock and hyperspace-charge flags remain immediate failures. Before visible
alignment, three consecutive UNKNOWN heat samples still fail closed. Failure
and cancellation own local STOP-hold, one charge-cancel command, and 0% throttle
compensation.

After Status confirms Supercruise, the Action maintains the aligned escape for
30 seconds while checking cruise and overheat every second. CV is sampled
every five seconds only as obstruction evidence. Completion leaves the ship in
Supercruise at 0% throttle and reports
`restoreHyperspaceDestinationRequired=true`.
