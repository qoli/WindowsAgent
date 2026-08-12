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
is 0% before acceleration is permitted. `ALIGN WITH TARGET DESTINATION` is a
generic current-frame target-alignment prompt even though `flight-status`
retains the historical `FSD_ALIGNMENT_REQUIRED` state name. It is not scoped
to the FSD charging phase. After Assist selection, that prompt starts a bounded
feedback cycle at 0% throttle: `align-station-target` first uses Compass for
coarse/rear-hemisphere alignment, then `align-visible-target` refines the
visible destination marker. The workflow re-reads the central prompt and
requires two consecutive samples without `FSD_ALIGNMENT_REQUIRED` before it
may restore 75% throttle. If the prompt remains, both alignment children run
again, for at most six cycles. A child `completed` result is therefore not the
Assist alignment postcondition; prompt disappearance is. Missing Compass or
visible-target evidence fails explicitly and never authorizes a blind sweep or
a different label.

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
page, and uses the existing same-frame OCR context to find `ACTIVATE
SUPERCRUISE ASSIST`, `DEACTIVATE SUPERCRUISE ASSIST`, or the shorter
`SUPERCRUISE ASSIST` contextual label. The detail icon label is contextual, so
the workflow sends exactly one `RIGHT` from BACK and requires the complete
mode-correct label in two consecutive observations. `ACTIVATE` or the shorter
label permits one `SELECT`; `DEACTIVATE` proves Assist is already active and
must not be selected because that would disable it. Missing or ambiguous text, focus, module, target
lock, or panel state fails explicitly. After the panel closes, the Action may
command 75% into the Supercruise blue zone. It gives the game three prompt
observations to react. If alignment remains required, it returns to 0%,
performs Compass alignment followed by visible-target alignment, and verifies
that the alignment prompt disappeared twice before restoring 75%; attitude
control never runs at blue-zone throttle. A prompt still present after either
child completed repeats the complete correction cycle instead of trusting the
child terminal state. `SUPERCRUISE ASSIST ACTIVE` must be classified twice
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
