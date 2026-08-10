# Elite Dangerous inter-system transit to station

This interruptible linear Streaming Action owns one visual single-hop journey
from an origin Station or confirmed normal space to one exact Station in one
exact destination System. A docked start delegates departure to `leave-station`;
its existing `AWAITING_AUTO_LAUNCH` event remains an explicit supervised UI
boundary. After departure, the parent owns every time-sensitive flight step.

It locks the exact visible destination System row, aligns at 0% through the
Compass and visible-target children, invokes only the binding-resolved
`HyperSuperCombination`, requires FSD charging, and then commands 100%.
Hyperspace transit is two consecutive cockpit-HUD-absent samples after charging.
Two consecutive returning cockpit samples command 0% on the confirming sample; arrival requires
two cockpit-present samples, two persistent Supercruise HUD samples, and two
exact destination-System target-text observations.

Because a hyperspace exit is already in Supercruise, the Action does not wait
for `ship-speed STOPPED`. It locks the exact destination Station, resumes
`supercruise-assist-to-destination` with `supercruiseConfirmed=true`, and then
calls `dock-at-station`, including its conditional safe-distance advance. The
terminal phase remains `VISUAL_CONFIRMATION_REQUIRED`.

The destination Station must be present in the current visible Navigation
list. A stale System-only filter remains an explicit missing-target failure;
the Action does not blindly manipulate the icon-only filter menu.

Missing target rows, missing bindings, ambiguous OCR, unconfirmed charging,
cockpit transition failure, missing Supercruise HUD, a destination-name
mismatch, child failure, or cancellation fails explicitly and commands 0%.
It never reads Journal, Status, NavRoute, network data, prior frames, or a
different perception pipeline, and it never retries FSD or substitutes the
dedicated Supercruise binding.
