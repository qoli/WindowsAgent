# Supercruise Assist to a locked destination

This interruptible linear Streaming Action uses Elite Dangerous' installed
Supercruise Assist computer for a `DROP` destination such as `NAV BEACON`. It
is separate from the retained manual `supercruise-to-destination` workflow.

The caller must first complete `select-and-lock-destination`, visually confirm
normal space, and confirm the ship's Supercruise Assist setting is `Auto
Throttle`. The Action checks Mass Lock, Landing Gear, and Cargo Scoop, then
invokes `align-station-target` with `targetMotion=STATIC`. Before Supercruise
entry it uses the strict `NORMAL_SPACE` Compass profile; after a confirmed
Supercruise entry it uses `SUPERCRUISE_ASSIST`. The Compass child remains the
coarse and initial alignment feedback source and must complete while throttle
is 0% before acceleration is permitted. Once the game explicitly emits
`FSD_ALIGNMENT_REQUIRED` after Assist selection, the destination is expected
in the forward HUD and the workflow switches to `align-visible-target` with
exact target identity, `searchWhenUnknown=false`, and the strict heat Gate.
This avoids a measured loop where Compass remained within 3-5 pixels while the
visible destination stayed off-centre and the game continued to reject Assist
ownership. Missing visible-target evidence fails; it never authorizes a blind
sweep or a different label.

The runtime supervises each nested alignment Streaming Action synchronously: its start,
events, completion, and failure are wrapped in the parent stream with a child
execution ID. Parent cancellation propagates through the shared context, and a
child failure fails the parent without a manual alignment fallback. The child
remains independently invokable. After alignment, the workflow starts the
dedicated Supercruise control and temporarily commands 100% only to enter
Supercruise. FSD charging must precede visual entry.

The dedicated `Supercruise` binding is one 80 ms press. During the entry loop,
`FSD_THROTTLE_UP_REQUIRED` is accepted as current OCR evidence that charging
reached its throttle handoff; the Action reissues 100% throttle and still waits
for independently observed Supercruise HUD entry. It does not treat that prompt
as completed entry or issue a blind second FSD toggle.

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
completes the supervised visible-target alignment child, and only then restores
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
