# Enter Planet Gravity Well

Enter normal space close enough to the currently locked planetary body that a
new Supercruise charge presents the game's `ALIGN WITH ESCAPE VECTOR` prompt.
That prompt, classified as `FSD_ESCAPE_VECTOR_REQUIRED`, is the terminal domain
proof; a distance threshold or a requested throttle is never treated as proof
of gravity-well entry.

The caller supplies only `targetName`. The Action itself requires current
`Status.json` evidence for an exact matching Destination and an idle FSD,
visually confirms Mass Lock, Landing Gear, and Cargo Scoop OFF, and starts from
0% throttle. From normal space it first performs a bounded zero-throttle FSD probe. If the Escape
Vector is already present, it cancels the owned charge and completes without
moving the ship. Two consecutive `THROTTLE UP TO ENGAGE` or ordinary
destination-alignment prompt classifications prove that the current charge is
not asking for Escape Vector, so the Action cancels immediately instead of
continuing to run the capture/OCR pipeline. This bounded early exit is also the
live WGC-pressure control for the probe. If current `Status.json` already proves
Supercruise, the Action takes over the approach directly: it must not toggle the
FSD, repeat coarse alignment, or leave Supercruise merely to recreate its own
entry transition.

Otherwise the Action aligns the static destination, enters manual Supercruise,
applies 100%, and observes the target-lock HUD distance plus the Supercruise
speed display. It treats three consecutive samples below 20 Mm and 20 km/s,
with a non-increasing target distance, only as a candidate drop point. It then
returns to normal space and repeats the FSD probe. Only confirmed Escape Vector
evidence completes the Action. Three consecutive increasing distance samples
prove that the ship is travelling away from the locked body; this sends 0% and
fails instead of continuing a wrong-direction 100% approach. Any failure or cancellation leaves a registered
0% throttle compensation; while this Action owns a charge, it additionally owns
one matching cancel command.

The game may force an automatic normal-space drop as the ship enters the body's
exclusion/gravity boundary before the visual speed reaches the candidate
threshold. A current `Status.json` transition from Supercruise to normal space
is therefore an explicit `DROPPING` branch: send 0%, stop requesting the now
absent Supercruise speed HUD, and proceed directly to the same Escape Vector
proof. The automatic drop is a strong transition event but is still not the
terminal gravity-well proof by itself.

This is a linear Streaming Action and is intentionally independent of
`clear-hyperspace-occlusion`. Its completed output is suitable evidence that
the latter Action's gravity-escape precondition exists. It does not select a
destination, perform hyperspace travel, land, or infer world geometry.
