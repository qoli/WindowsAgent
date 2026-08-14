# Supercruise Assist to a locked destination

This interruptible linear Streaming Action uses Elite Dangerous' installed
Supercruise Assist computer for a `DROP` destination such as `NAV BEACON`. It
is separate from the retained manual `supercruise-to-destination` workflow.

The caller must first complete `select-and-lock-destination`, visually confirm
normal space, and confirm the ship's Supercruise Assist setting is `Auto
Throttle`. The Action checks Mass Lock, Landing Gear, and Cargo Scoop through
a four-sample bounded preflight window. Any observed `ON` fails immediately;
two consecutive observations with all three `OFF` pass; unresolved `UNKNOWN`
remains unknown and fails only after the window is exhausted. It then invokes
`align-station-target` followed by `align-visible-target` with the named
destination. The Compass child owns the
rule that `NAV BEACON` is always `targetMotion=MOVING`; ordinary stations retain
the requested `STATIC` profile. Before Supercruise
entry it uses the strict `NORMAL_SPACE` Compass profile with the
`HYPERSPACE_CHARGE` purpose, which requires the tighter normal-space pre-FSD
Gate; the visible child then requires the current destination focus frame to
complete its `DESTINATION`/`STRICT` screen-centre Gate. Both children must
complete at 0% throttle before Supercruise input or acceleration is permitted.
If charging later reports `FSD_ALIGNMENT_REQUIRED`, the Action returns to 0%,
repeats the same Compass-to-visible pair, and restores 100% only after both
children complete. A Compass handoff alone never authorizes charging recovery.
After a confirmed Supercruise entry it uses `SUPERCRUISE_ASSIST` with
the explicit `VISIBLE_HANDOFF` purpose. The Compass child owns rear/coarse
entry into the appropriate front SOLID domain; `align-visible-target` owns any
following precise screen-centre Gate. A resumed invocation already in
Supercruise uses the same pair with the `SUPERCRUISE_ASSIST` profile before it
continues to the Assist UI. `ALIGN WITH TARGET DESTINATION` is a
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
as completed entry or issue a blind second FSD toggle. The entry window permits
45 observations because a live T9 charge can still be displaying its final
countdown after the previous 30-observation bound. Exhausting the larger bound
still fails explicitly and invokes the registered 0% throttle compensation;
elapsed samples never substitute for two fresh persistent-HUD observations.
Each initial or charging-recovery pair emits a durable
`ALIGN_STATION_TARGET+ALIGN_VISIBLE_TARGET` summary with both child sample
counts; neither child terminal result is hidden as completion of the pair.

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
lock, or panel state fails explicitly. The Navigation detail view does not
expose the tab-header pixels used by `left-panel-tab-state`, so an `ABSENT` tab
result is not accepted as proof that detail closed. The workflow must call
`close-navigation-detail`, which sends BACK, closes the returned Navigation
list, and independently reports `panelClosed=true` and `finalState=ABSENT`.
Only that postcondition permits 75%; the detail-close compensation remains
registered until it passes.

Immediately before opening Navigation, the Action takes one current
`flight-status` snapshot. Detail-card text overlaps the central prompt ROI, so
every `FOCUSING_ASSIST`, `LOCATING_ASSIST`, and `REQUESTING_ASSIST` event carries
that value with `flightStatusSource=PRE_NAVIGATION_SNAPSHOT` instead of
misclassifying detail prose as a current flight prompt. After the panel-close
postcondition, `CLOSING_PANEL` immediately resumes a fresh observation marked
`CURRENT_FRAME`. Domain insufficiency remains explicit `UNKNOWN`; the Action
never reports `null` or disguises the snapshot as current. The detail-close
failure compensation is bounded to 5 seconds.

If that restored current frame already reports
`SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED`, it is retained as the first LOS
Gate confirmation while throttle remains 0%. A second fresh observation must
confirm the Gate before the clear-LOS child starts. The Action must not request
75% between those two observations; if the second frame rejects the candidate,
only then may it request the blue-zone throttle.

After 75% is requested, six consecutive observations without any ACTIVE,
alignment-required, or line-of-sight-required Gate are a bounded unsafe
ownership gap. The Action immediately commands 0%, emits
`ASSIST_OWNERSHIP_TIMEOUT` with the missing-sample count, and fails instead of
continuing to move while repeatedly reporting `WAITING_FOR_ASSIST`. A single
alignment or line-of-sight candidate pauses but does not reset this counter;
only a fully debounced and accepted Gate resets it and enters its owning
recovery path. It gives the game three prompt
observations to react. If alignment remains required, it returns to 0%,
performs Compass alignment followed by visible-target alignment, and verifies
that the alignment prompt disappeared twice before restoring 75%; attitude
control never runs at blue-zone throttle. A prompt still present after either
child completed repeats the complete correction cycle instead of trusting the
child terminal state. `SUPERCRUISE ASSIST ACTIVE` must be classified twice
before the game computer owns flight.

After ownership begins, the Action normally sends no throttle, attitude, UI,
or FSD input. It only watches `flight-status` and `ship-speed`. The sole
ordinary recovery exception is two consecutive
`SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` samples. That Gate means the game
computer cannot continue because the selected destination is physically
occluded; it is not ordinary Assist disappearance and must not accumulate
toward `ASSIST_INTERRUPTED`.

At that Gate the parent commands 0% and synchronously runs the independent
`clear-supercruise-assist-line-of-sight` Streaming Action. That child must
confirm the obstruction body left the viewport, fly outward for the complete
30-second separation lease, and return at 0%. The parent then owns the explicit
4.5 handoff in this exact order: Compass coarse alignment, confirmed Compass
handoff, and visible focus-frame fine alignment. Durable phases expose all
three steps before the parent re-reads the original prompt twice. `UNKNOWN`
does not count as prompt absence. If the line-of-sight prompt returns, the
complete escape, separation, and realignment cycle repeats, bounded to three
recoveries. Only after positive prompt-clear verification may the parent
restore 75% and require two fresh `SUPERCRUISE ASSIST ACTIVE` samples before
treating the game computer as flight owner again. The output records the
recovery count and truthfully sets `agentFlightInputAfterAssistActive=true`
when this explicit recovery path issued flight input. No child completion
substitutes for the original OCR Gate.

The independent `orbital-scale-gauge-state` observation is a higher-priority
safety Gate throughout preflight, entry, Assist acquisition, correction,
line-of-sight handoff, and game-controlled approach. It applies the reviewed
orange heading-scale geometry in the current frame and retains its evidence
score. One `DETECTED` frame is sufficient to command 0% immediately; no later
75% or 100% command is permitted. The parent then invokes
`pause-at-exit-for-human-takeover`, which opens the pause menu, clamps focus to
`EXIT`, and requires two fresh visual confirmations of that exact handoff
screen. It deliberately does not select EXIT. Once the handoff is confirmed,
the parent emits `HUMAN_TAKEOVER` and fails with the stable
`NEAR_ORBIT_SAFETY_TRIGGERED` prefix because the requested autonomous flight
goal was aborted. A capture, schema, OCR, or menu-observation failure remains
terminal and does not become an absent scale or a successful handoff.

Up to five transient
failures from the same persistent WGC region-capture provider may be skipped
while the game owns flight; the sixth consecutive failure, or any different
observation error, remains terminal. A successful speed observation resets
this counter. `SAFE DISENGAGE READY`
is observational and never triggers a manual FSD command. Completion requires
the Assist indication to have disappeared for three frames and three
consecutive slashed-zero-backed `STOPPED` frames, proving the game's automatic
drop and stop. Thirty missing Assist samples without that stop fail as
`ASSIST_INTERRUPTED`; no manual-flight fallback is attempted. Failure or
cancellation still invokes the registered 0% throttle compensation.

`destinationMode=DROP` accepts only `SUPERCRUISE ASSIST`; mode selection does
not bypass the orbital-scale safety Gate. `destinationMode=ORBIT_HANDOFF`
accepts only `SUPERCRUISE ASSIST AND ORBIT`, but ownership confirmation is no
longer a terminal success: the Action remains responsible until either its
ordinary observed stop completes or the orbital-scale Gate produces the
explicit paused human-takeover terminal.
