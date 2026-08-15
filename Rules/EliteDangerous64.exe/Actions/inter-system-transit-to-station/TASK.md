# Elite Dangerous inter-system transit to station

This interruptible linear Streaming Action owns one visual single-hop journey
from an origin Station, confirmed normal space, or confirmed Supercruise to one
exact Station in one exact destination System. A docked start delegates departure to `leave-station`;
its existing `AWAITING_AUTO_LAUNCH` event remains an explicit supervised UI
boundary. After departure, the parent owns every time-sensitive flight step.
`SUPERCRUISE` is an explicit start mode with its own confirmation input and
uses the Supercruise Compass control profile; it is never inferred from a
`NORMAL_SPACE` claim.

Before the jump it delegates complete-name Galaxy Map search, exact suggestion
selection, held `PLOT ROUTE`, and exact one-hop `NavRoute.json` validation to
`plot-route-to-system`. That durable route result is the explicit target-lock
boundary passed to the jump child; the parent no longer asks the current-System
Navigation list to find an arbitrary unplotted System.

It delegates the resulting exact target lock, 0% alignment, binding-resolved
`HyperSuperCombination`, FSD charging, hyperspace transition, first-returning-
cockpit-frame 0% brake, and persistent Supercruise arrival evidence to
`hyperspace-jump-to-system`. The jump child's exact matching `FSDJump` or
bounded cockpit-transition arrival evidence authorizes the Station phase. It
deliberately does not require the hyperspace destination label to remain
visible after arrival: Elite Dangerous may clear that target together with
`NavRoute.json` once the jump completes.

Every ordinary arrival and `ARRIVED_SUPERCRUISE` recovery then performs an
unconditional arrival-star separation before the Station is selected. The
parent calls `clear-hyperspace-occlusion` in its existing-Supercruise mode and
requires the shared mechanical contract: two fresh compatible sphere
directions, a fixed 6400 ms outward turn, a fixed 30000 ms separation flight,
persistent Supercruise, and final commanded throttle 0%. No colour-coverage
`CLEAR`, empty detector frame, or sphere disappearance can skip this segment
or authorize thrust. The Station remains deliberately unlocked until the
segment completes because the outward turn changes the ship attitude.

`ARRIVED_SUPERCRUISE` is the explicit recovery entry after a parent-level
failure in the destination System. It requires the latest retained `FSDJump`
to match `destinationSystem` case-insensitively but otherwise exactly and two
current Supercruise HUD observations,
then resumes at arrival-star separation without plotting or repeating the
jump.

Because a hyperspace exit is already in Supercruise, the Action does not wait
for `ship-speed STOPPED`. It locks the exact destination Station, resumes
`supercruise-assist-to-destination` with `supercruiseConfirmed=true`, and then
calls `dock-at-station`, including its conditional safe-distance advance. The
terminal phase remains `VISUAL_CONFIRMATION_REQUIRED`.

The destination Station must be present in the current visible Navigation
list. A stale System-only filter remains an explicit missing-target failure;
the Action does not blindly manipulate the icon-only filter menu.

Missing or partial Galaxy Map results, a route mismatch, missing Station rows,
missing bindings, ambiguous OCR, unconfirmed charging,
cockpit transition failure, missing Supercruise HUD, a destination-name
mismatch, child failure, or cancellation fails explicitly and commands 0%.
It reads `NavRoute.json` only through the owning route child and never uses
network data, prior frames, or a different perception pipeline. It never
retries FSD or substitutes the dedicated Supercruise binding.
