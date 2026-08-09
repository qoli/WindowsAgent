# Supercruise Assist to a locked destination

This interruptible linear Streaming Action uses Elite Dangerous' installed
Supercruise Assist computer for a `DROP` destination such as `NAV BEACON`. It
is separate from the retained manual `supercruise-to-destination` workflow.

The caller must first complete `select-and-lock-destination`, visually confirm
normal space, and confirm the ship's Supercruise Assist setting is `Auto
Throttle`. The Action checks Mass Lock, Landing Gear, and Cargo Scoop, aligns
the Compass, starts the dedicated Supercruise control, and temporarily commands
100% only to enter Supercruise. FSD charging must precede visual entry.

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
observations to react before using the separate forward-HUD target-position
alignment when alignment remains required. `SUPERCRUISE ASSIST ACTIVE` must be classified twice
before the game computer owns flight.

After ownership begins, the Action sends no throttle, attitude, UI, or FSD
input. It only watches `flight-status` and `ship-speed`. `SAFE DISENGAGE READY`
is observational and never triggers a manual FSD command. Completion requires
the Assist indication to have disappeared for three frames and three
consecutive slashed-zero-backed `STOPPED` frames, proving the game's automatic
drop and stop. Thirty missing Assist samples without that stop fail as
`ASSIST_INTERRUPTED`; no manual-flight fallback is attempted. Failure or
cancellation still invokes the registered 0% throttle compensation.

The first version intentionally supports only `destinationMode=DROP`.
`SUPERCRUISE ASSIST AND ORBIT` is rejected instead of being interpreted with
the wrong completion Gate.
