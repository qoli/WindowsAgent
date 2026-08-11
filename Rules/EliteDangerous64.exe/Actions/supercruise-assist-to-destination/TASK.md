# Supercruise Assist to a locked destination

This interruptible linear Streaming Action uses Elite Dangerous' installed
Supercruise Assist computer for a `DROP` destination such as `NAV BEACON`. It
is separate from the retained manual `supercruise-to-destination` workflow.

The caller must first complete `select-and-lock-destination`, visually confirm
normal space, and confirm the ship's Supercruise Assist setting is `Auto
Throttle`. The Action checks Mass Lock, Landing Gear, and Cargo Scoop, then
invokes `align-station-target` with its `SUPERCRUISE_ASSIST` control profile
for Compass-based coarse alignment, then invokes `align-visible-target` for
the OCR-derived forward-HUD centre Gate. Both children must complete while
throttle remains at 0% before acceleration is permitted. Compass owns coarse
holds, 80 ms fine pulses inside 40 reference pixels, and a bounded 1000 ms
no-movement recovery pulse; visible-target alignment supplies the final
screen-space proof. `supercruise-assist-to-destination` does not contain a
third attitude-control loop.

When nearby HUD contacts pollute Compass coarse evidence and the exact locked
destination remains outside the OCR bands, the visible child is invoked with
its bounded exact-name search enabled. The child may sweep yaw while throttle
remains 0%, but it cannot accept another label or bypass its heat Gate.

The runtime supervises this nested Streaming Action synchronously: its start,
events, completion, and failure are wrapped in the parent stream with a child
execution ID. Parent cancellation propagates through the shared context, and a
child failure fails the parent without a manual alignment fallback. The child
remains independently invokable. After alignment, the workflow starts the
dedicated Supercruise control and temporarily commands 100% only to enter
Supercruise. FSD charging must precede visual entry.

Only after Supercruise entry does the Action command 0% minimum Supercruise
throttle and reopen NAVIGATION. This avoids racing the version-dependent UI
behavior that may attempt to start FSD when Assist is selected in normal space.
It requires the named target's angle brackets and focused row, opens its detail
page, and uses the existing same-frame OCR context to find `SUPERCRUISE
ASSIST`. The detail icon label is contextual, so the workflow sends exactly
one `RIGHT` from BACK and requires that label in two consecutive observations
before `SELECT`. Missing or ambiguous text, focus, module, target
lock, or panel state fails explicitly. After the panel closes, the Action may
command 75% into the Supercruise blue zone. It gives the game three prompt
observations to react. If alignment remains required, it returns to 0%,
completes the same supervised target-alignment child, and only then restores
75%; attitude control never runs at blue-zone throttle. `SUPERCRUISE ASSIST
ACTIVE` must be classified twice
before the game computer owns flight.

After ownership begins, the Action sends no throttle, attitude, UI, or FSD
input. It only watches `flight-status` and `ship-speed`. `SAFE DISENGAGE READY`
is observational and never triggers a manual FSD command. Completion requires
the Assist indication to have disappeared for three frames and three
consecutive slashed-zero-backed `STOPPED` frames, proving the game's automatic
drop and stop. Thirty missing Assist samples without that stop fail as
`ASSIST_INTERRUPTED`; no manual-flight fallback is attempted. Failure or
cancellation still invokes the registered 0% throttle compensation.

`destinationMode=DROP` accepts only `SUPERCRUISE ASSIST` and retains the
automatic drop-and-stop completion contract. `destinationMode=ORBIT_HANDOFF`
accepts only `SUPERCRUISE ASSIST AND ORBIT` and completes at the explicit
`ASSIST_HANDOFF` boundary after two visual ownership confirmations. It does
not claim that the body has been reached or that the ship left Supercruise;
those are separate distance/arrival and drop Actions.
