# Elite Dangerous Agent Rule

Use this Rule only when a fresh WindowsAgent capture reports:

- `foreground.executable_name: EliteDangerous64.exe`
- `rule.status: matched`
- `rule.id: EliteDangerous64.exe`

## Current capability boundary

Read the exact `rule.actions.url` and `rule.registrations.url` from the capture
before using a game-specific capability.

`elite-dangerous/compass` is a finite, directly callable Action. It owns a
fixed compass region in the 1920x1080 reference coordinate space and its HUD
color interpretation. The game-neutral Observer maps that region through a
centered 16:9 viewport and returns reference-density pixels. A changed
foreground process or invalid compass evidence fails explicitly. Its target
evidence also classifies the reviewed cyan topology as `SOLID`/front,
`HOLLOW`/rear, or evidence-preserving `UNKNOWN`. Preprocessing builds a
brightness-tolerant cyan response mask and removes isolated pixels without
closing the hollow center. Marker bounds establish a topology center: a filled
three-by-three center means front, while an empty center inside a sufficiently
large ring means rear. Candidate/retained counts, the five-by-five core, and
marker bounds remain visible diagnostic evidence; total cyan count alone never
decides the hemisphere.

`elite-dangerous/ship-attitude-control` is the finite binding-resolved flight
primitive. It injects exactly one `PITCH_UP`, `PITCH_DOWN`, `YAW_LEFT`,
`YAW_RIGHT`, `ROLL_LEFT`, or `ROLL_RIGHT` press from the active Frontier preset.
The caller may explicitly request a 40 through 1000 ms hold; omission uses the
declared 40 ms default. Frontier's `Key_*Arrow` names map to the same extended
Windows scan codes as their canonical directional-key counterparts. Successful
output proves key injection only, not attitude movement.

`elite-dangerous/ship-attitude-hold` is the non-blocking counterpart for one
coarse attitude axis. `START` returns a 2500 ms lease after key-down, `RENEW`
extends that exact lease, and `STOP` releases it. Only one hold lease may be
active; ordinary press Actions fail while it remains active. Streaming failure
compensation, lease expiry, and Agent shutdown release the resolved key.

`elite-dangerous/ship-attitude-vector-hold` owns one pitch-plus-yaw pair under
the same lease contract. START resolves and presses both keys into an
overlapping hold; STOP, expiry, partial START failure, and Agent shutdown
release the pair together. Its grouped output exposes both controls, keys,
scan codes, and extended-key flags.

Elite Dangerous has a reproduced startup input-initialization failure: when the
configured controller was off during a cold game start, binding-resolved
`PITCH_UP` injections completed but produced no visual or Compass movement,
while Yaw remained functional. Powering on the controller restored the same
Pitch command immediately without restarting the game. XInput enumeration
still did not expose that controller, so it is not a valid readiness Gate. When
`align-station-target` reports `ED_PITCH_INPUT_CONTEXT_NOT_READY`, do not begin
another binding, scan-code, or Compass investigation. Report the included
`information` response, have the controller powered on or reconnected, and
retry the Streaming Action without restarting Elite Dangerous.

`elite-dangerous/align-station-target` is an interruptible linear Streaming
Action over the selected target. By default it first commands 0% throttle. A
hollow rear marker or solid marker farther than 40 reference pixels uses one
leased sustained control while Compass is sampled at a one-second
start-to-start cadence. It releases the hold on hemisphere, axis, or fine-band
transition. A far solid marker with meaningful error on both axes uses the
compound pitch-plus-yaw lease for diagonal movement. Inside 40 pixels it
returns to distance-scaled bounded pulses at the same cadence. Events expose
control mode, lease state, sample timing, requested
pulse duration, observed marker movement, distance delta, moving-away trend,
and consecutive no-movement count. Four stationary samples
or five consecutive front samples moving away fail explicitly. Three
consecutive solid samples in the four-pixel center zone are required for
completion. Its structured update events are the durable control timeline;
explicit activity events supply the Action OSD. An owning flight workflow may
set `stopBeforeAlign=false` when it already controls throttle. It does not establish Station
target lock, approach the Station, request docking, or participate in the
docking-computer workflow.

`elite-dangerous/flight-prompt-text` is a second finite Action. It captures the
reviewed central prompt as a 400x40 reference-density RGB line and sends it to
the Rule-declared `ocr/w480` resident DirectML worker. Its output is
raw OCR text evidence only.

`elite-dangerous/flight-status` is its finite, pure post-processing Action. It
accepts the complete raw OCR output and combines OCR confidence with reviewed
phrase similarity. A candidate must also meet the explicit `0.60` phrase
similarity floor; high-confidence unrelated OCR remains `UNKNOWN`. It returns `SUPERCRUISE`, `AUTO_LAUNCH`,
`WAITING_IN_QUEUE`, `SLOW_DOWN_FOR_AUTO_DOCK`, `FSD_CHARGING`,
`FSD_ALIGNMENT_REQUIRED`, `SUPERCRUISE_ASSIST_ACTIVE`,
`SAFE_DISENGAGE_READY`, `AUTO_DOCK`, or
evidence-preserving `UNKNOWN`. It
performs no capture or OCR and malformed raw input fails schema validation.
Multi-frame confirmation, event emission, and follow-up execution remain
registration concerns.

`elite-dangerous/ship-status` is a finite composite Action over the reviewed
lower-right HUD region. Its internal raw Action captures at reference density
and uses the Rule-declared `ocr/text-regions` resident DirectML worker to return
PP-OCR quadrilateral boxes and recognition evidence. A pure classifier confirms
only `MASS`, `LANDING`, and `CARGO`, then independently returns `massLock`,
`landingGear`, and `cargoScoop` as cyan `ON`, orange `OFF`, or evidence-preserving
`UNKNOWN`. It never falls back to the retired fixed-position triplet detector.

`elite-dangerous/ship-speed` is a finite composite Action over the fixed visual
speed-number region. It returns the displayed number only as `KNOWN` when the
digit-constrained CTC candidate contains one through four digits, reaches the
reviewed confidence threshold, and remains sufficiently close to the retained
unrestricted candidate; otherwise it returns `UNKNOWN`. It does not run the
text-region detector. Its value is observed HUD evidence, not the requested
throttle setting. The unit remains unknown, and the Action never consults
Player Journal, `Status.json`, prior commands, or another fallback source. It
may be registered as a Monitor, but no speed loop is active by default.

Target geometry uses 1080p reference pixels. `screenAngleDegrees` is clockwise
from straight up. `centerZone.inside` is a current-frame circular membership
state; do not infer an entered/exited transition without Monitor history.

These five observation Actions declare that they may be registered as either a
Monitor or Reaction, but the registration catalog is intentionally empty. Do not infer a
timer or event subscription from `registrableAs`; declaring eligibility does
not activate a registration. Likewise, `ocr/w480` and `ocr/text-regions`
residency only keep model initialization alive while this Rule owns the
foreground; neither invokes an OCR Action nor produces an event.

`elite-dangerous/request-docking-range` is a finite composite Gate Action. Its
internal `request-docking-distance-regions` Action scans the reviewed horizontal
lower-left HUD band at reference density with the Rule-resident PP-OCR text
regions worker, then the pure `request-docking-range-classifier` selects exactly
one current distance region. It returns `ALLOWED` only when detection and
recognition confidence pass their declared thresholds and the displayed value
is strictly below `7500m`; `7.50km` is `DENIED`. Missing, malformed,
low-confidence, or ambiguous evidence is `UNKNOWN`, never a prior-frame or
inferred value. This path does not use ScreenParser, repair malformed units, or
fall back to the retired fixed distance ROI. Run it in the settled forward
cockpit view before opening the Target panel. The Action is not registrable and
never performs the docking request itself.

`elite-dangerous/request-docking-availability` is a finite composite visual
Gate for the currently selected Contacts target. Its raw text-regions Action
scans a broad detail-panel area; PP-OCR dynamically locates the stable
`FACTION` heading and the action lines in the same frame. The classifier only
accepts Request/Cancel candidates inside a bounded zone relative to that
anchor, so a shifted cockpit view is not interpreted through a fixed action-row
crop or a later capture. The pure classifier distinguishes
`REQUEST DOCKING` from `CANCEL DOCKING` and reads the matched line's same-frame
left context to tell a visible dark row from the bright fill that means keyboard
focus is on the row. It returns `AVAILABLE`, `FOCUSED`, `UNAVAILABLE`,
`DOCKING_ACTIVE`, or `UNKNOWN`. A selected target is never assumed to support
docking. Only `FOCUSED`, together with the independent allowed range Gate,
permits a later `SELECT`; the Action itself never navigates or injects input.

`elite-dangerous/dock-at-station` is a linear Streaming Action. Its range Gate
watches indefinitely and builds a temporal distance trend: two readings within
`1000m` establish continuity, a larger one-frame jump is rejected, and two
mutually continuous readings can rebase the track. Admission requires at least
three trusted trend samples followed by two accepted `ALLOWED` samples;
`DENIED` and `UNKNOWN` remain visible waiting states. After its one-time range admission
and verified docking request, monitoring uses explicit
`action.try_call` results. A failed child observation is written as
`OBSERVATION_ERROR`, does not advance the prompt-disappearance or Landing Gear
Gates, and fails the workflow after three consecutive errors. It never changes
capture or state providers.

`elite-dangerous/ui-control` is a finite slow-interaction primitive. A
supervising model chooses exactly one logical `FOCUS_LEFT_PANEL`, `NEXT_PANEL`,
`PREVIOUS_PANEL`, `UP`, `DOWN`, `LEFT`, `RIGHT`, `SELECT`, or `BACK` after
inspecting a fresh screenshot.
`FOCUS_LEFT_PANEL` resolves the dedicated Frontier control; `LEFT` remains
in-panel navigation. `NEXT_PANEL` and `PREVIOUS_PANEL` resolve the dedicated
Frontier panel-cycle controls and must not be replaced with `LEFT` or `RIGHT`.
The runtime resolves that logical control from the game's active binding preset, then uses the
game-neutral Windows scan-code input driver; never assume Space or any other
fixed physical key. Successful output includes the binding source, backend,
scan code, extended-key flag, and configured hold time.

`elite-dangerous/left-panel-tab-state` is a finite observation Action. It scans
four fixed `4x4` header squares and returns the active member of the four-state
cycle: `SYSTEM`, `NAVIGATION`, `TRANSACTIONS`, or `CONTACTS`. `SYSTEM` is the
icon-only overview Tab immediately left of `NAVIGATION`. Missing header evidence
returns `ABSENT`; conflicting or insufficient highlight evidence returns
`UNKNOWN`. It does not OCR the header or infer state from prior samples.

`elite-dangerous/select-contacts-panel` is an interruptible linear Streaming
Action. It opens a stably absent left panel at most once, invokes only the
dedicated `NEXT_PANEL` control, and verifies every transition with two current
header pixel scans. The four tabs cycle as `SYSTEM`, `NAVIGATION`,
`TRANSACTIONS`, `CONTACTS`, then back to `SYSTEM`. It completes
only after `CONTACTS` is confirmed twice. Its scope ends before selecting a
contact, focusing `REQUEST DOCKING`, or requesting docking. Follow its returned
watch URL; unknown evidence or failure to reach Contacts within three cycles
terminates the Action.

`elite-dangerous/select-station-target` is a separate interruptible linear
Streaming Action. It is not a child phase of `dock-at-station`. The caller must
provide the exact Station name. In CONTACTS, a filled row proves keyboard focus
only; angle brackets around the recognized row text, for example
`< MOONGLOW CITY >`, are the direct Station target-lock evidence. The Action
sends `SELECT` only after two current OCR observations prove the named visible
row is focused. It then requires two current angle-bracketed observations
before reporting `ACQUIRED`. If four post-input observations remain on the
same confirmed focused row, it emits that the game rejected the input and
permits exactly one second `SELECT`; an ambiguous transition or second
rejection fails explicitly. An already bracketed row reports `EXISTING`
without input. A missing visible target, ambiguous OCR, or unknown focus fails
explicitly and never falls back to the first CONTACTS row. The Action restores
the forward view only when it opened the left panel itself.

`elite-dangerous/lock-destination` is a separate interruptible linear
Streaming Action for an already-open Navigation detail card. The higher agent
must choose and open the intended Navigation row first. The Action combines a
fixed reference-density primary-button fill scan with dedicated OCR of `LOCK
DESTINATION` versus `UNLOCK DESTINATION`. It requires two consecutive focused
known observations before input. `UNLOCK DESTINATION` reports `EXISTING`
without pressing it; `LOCK DESTINATION` sends `SELECT` once and requires two
angle-bracketed OCR observations of the supplied `targetName` from the
Navigation-list-specific w480 ROI before reporting `ACQUIRED`. It does not scan
the list, choose a destination, or close a panel it did not open.

`elite-dangerous/select-and-lock-destination` is the complete name-driven
Navigation workflow. Given an exact currently visible `targetName`, it opens
the left panel when needed, anchors on the observed CONTACTS tab, moves to
NAVIGATION, locates and focuses the named OCR row, opens its detail card,
activates only a confirmed focused `LOCK DESTINATION`, verifies two
angle-bracketed named-row observations, then always closes the left panel and
requires two current `ABSENT` observations before completion. `openedPanel`
only reports whether the panel was initially absent; an already-open panel is
not left behind after success. Use this Action instead of asking the higher model to perform
the panel and row-selection prelude. Missing, off-screen, ambiguous, or
unfocused evidence fails explicitly; it never scrolls blindly or chooses a
different target.

`elite-dangerous/dock-at-station` is the complete interruptible linear docking
workflow. It owns view normalization, the watched `request-docking-range`
admission Gate, CONTACTS navigation, Request Docking focus and one-shot
selection, `CANCEL DOCKING` verification, panel closure, throttle-zero handoff,
and the subsequent `AUTO_DOCK` plus Landing Gear monitor. After range admission
it never samples or reasons about distance again. It completes only with the
domain phase `VISUAL_CONFIRMATION_REQUIRED`, after Auto Dock was observed and
then stably disappeared while Landing Gear remained ON; a supervising model
must still inspect the final current frame before claiming the ship is docked.

### Visual focus confirmation for the higher execution Agent

Do not infer keyboard focus from one color or one static frame. Elite
Dangerous uses its configured HUD color throughout the interface, so orange or
bright-yellow appearance alone is not a focus rule. Before navigating, the
higher execution Agent must first identify and calibrate the current menu's
highlight state from a fresh screenshot and a reversible, one-step visual
test. Relevant evidence may include a changed fill, border, brightness, text
contrast, glyph treatment, or contextual label, but only a change synchronized
with a completed directional input establishes which treatment means focused
in the current UI state.

Focus is not confined to the vertical text menu. It may be on the icon row
above it, so calibration must include both areas. In one reviewed default-color
station sequence, the `RETURN TO SURFACE` elevator icon initially had a filled
tile. One `UP` produced no visible movement at that boundary. One subsequent
`DOWN` moved the differing tile treatment to `STARPORT SERVICES`; a second
`DOWN` moved it to `AUTO LAUNCH`. This sequence demonstrates how to calibrate
focus through correlated movement; its bright-yellow color is an observation,
not a reusable rule.

Use the following evidence loop before `SELECT`:

1. Capture a fresh frame and describe the candidate highlight treatments,
   including the icon row as well as the text rows. Do not yet declare which
   candidate is focused from color alone.
2. Send exactly one reversible directional `ui-control` invocation, wait for
   it to return, then capture a new frame. Associate the new frame only with
   that completed invocation.
3. Compare the two frames and identify whether one coherent highlight treatment
   moved from one control to an adjacent control. Use that synchronized visual
   delta to calibrate the focused and unfocused appearances for the current
   menu state.
4. Navigate one direction and one fresh frame at a time. Invoke `SELECT` only
   when the target has the calibrated focused appearance in the newest frame.

Do not capture the navigation result in the middle of the Elite Dangerous UI
animation. After each completed finite input, allow approximately one second
for the panel or focus transition to settle before requesting the evidence
frame. The capture artifact creation time includes later HDR conversion and PNG
encoding and is not the frame acquisition time; neither an immediate animation
frame nor the later artifact timestamp proves the settled UI state.

The Target panel remembers its last tab. To reach station docking controls,
invoke `FOCUS_LEFT_PANEL`, inspect the settled frame, and navigate to `CONTACTS`
only if another tab is visibly active. Select a nearby Starport with one
`UP`/`DOWN` input and one settled frame at a time. A selected contact alone is
not a docking request. Continue only when a current frame exposes the contact's
`REQUEST DOCKING` action. In a reviewed in-range flight state, the Starport row
was already focused when `CONTACTS` opened and the action was visible but not
focused; one `RIGHT` changed the action from a dark row to the calibrated filled
highlight. That observed transition justifies `SELECT` for that state, but does
not replace current-frame calibration. Never assume a fixed tab count, contact
row, `RIGHT` step, or `SELECT` sequence. When already docked, the Starport can
still appear in `CONTACTS` but `REQUEST DOCKING` is unavailable; that state is
not valid evidence for the final action path.

After selecting the focused action, require a settled success transition. The
reviewed successful request replaced `REQUEST DOCKING` with `CANCEL DOCKING`
and simultaneously displayed `DOCKING REQUEST GRANTED`. Either input completion
or the panel closing is insufficient. If those current visual confirmations are
missing, keep the request result `UNKNOWN`; do not issue another blind `SELECT`,
which could activate a different current action or cancel an accepted request.

A completed `ui-control` result proves only that WindowsAgent resolved and
injected the configured key. It does not prove that the game accepted the key
or moved focus. If the new frame is unavailable, a capture times out, multiple
inputs could fall between frames, or no focus delta is visible, report focus as
`UNKNOWN`; do not reuse an older frame and do not invoke `SELECT`. After
`SELECT`, treat the item as activated only after a fresh visual transition or
the relevant observation Action provides matching evidence. Input completion
alone is not activation or workflow completion.

`elite-dangerous/set-throttle` is deterministic. It accepts only `-100`, `0`, `75`, or `100`,
resolves the corresponding logical throttle control from the active preset on
each invocation, and reports the exact resolved key and injection evidence.

`elite-dangerous/supercruise-control` is the dedicated finite FSD primitive. It
resolves only Frontier's `Supercruise` control. It does not fall back to
`HyperSuperCombination`, which could initiate a hyperspace jump when a route
target is active. Missing or ambiguous Keyboard bindings fail explicitly.

`elite-dangerous/supercruise-to-destination` is an interruptible linear
Streaming Action from an already confirmed Navigation destination lock to a
safe normal-space arrival. The caller must first complete
`select-and-lock-destination`, then pass the same target name with
`targetLocked=true` and `normalSpaceConfirmed=true`; nested Streaming Actions are deliberately not hidden
inside the workflow. It visually requires Mass Lock, Landing Gear, and Cargo
Scoop all OFF, stops and aligns the ship, enters Supercruise through the
dedicated binding, and requires FSD charging followed by `SUPERCRUISE` OCR evidence. Its approach uses
the configured 75% throttle binding while correcting a solid Compass marker
outside 16 reference pixels. Two consecutive `SAFE_DISENGAGE_READY` frames are
the only disengage Gate. After toggling FSD and commanding 0%, three consecutive
slashed-zero-backed `ship-speed` `STOPPED` observations are required for
completion. Once FSD movement may begin, every failure or cancellation has a
registered 0% throttle compensation.

`elite-dangerous/supercruise-assist-to-destination` is the separate game-
computer workflow for `destinationMode=DROP`, initially targeting `NAV BEACON`.
It requires the caller to have confirmed the destination lock, normal space,
and the ship's `Auto Throttle` Assist setting. It first enters Supercruise
manually, then commands minimum Supercruise throttle before opening the locked
target's Navigation detail. Two OCR frames must identify the non-orbit
`SUPERCRUISE ASSIST` action. The detail icon label is contextual, so the Action
sends one `RIGHT` from BACK and treats two matching label frames as focus
evidence before `SELECT`. Missing module/button/focus
evidence fails without a manual-flight fallback. The workflow may align only
after commanding the configured 75% blue-zone throttle and observing a
persistent alignment requirement. Two `SUPERCRUISE_ASSIST_ACTIVE` frames then transfer ownership to the game.
After that transfer it emits observations but sends no throttle, attitude, UI,
or FSD input. Completion is the conjunction of three missing-Assist frames and
three slashed-zero `STOPPED` frames. Thirty missing-Assist frames without the
stop are `ASSIST_INTERRUPTED`. `SAFE_DISENGAGE_READY` remains observational;
the Action never manually toggles FSD on arrival. `ASSIST AND ORBIT` is rejected
until a separate orbit completion contract exists.

After the dedicated Supercruise input and 100% throttle command, entry evidence
is bounded to thirty OCR observations. Persistent `UNKNOWN` prompt evidence is
not progress: the Action fails and its registered 0% throttle compensation
runs instead of leaving the ship moving in normal space for the longer Assist
activation window.

`elite-dangerous/leave-station` is an interruptible linear Streaming Action.
Invoke it with `{"stationConfirmed":true}` only after high-level visual
confirmation that the ship is inside a station, then follow the returned NDJSON
watch URL. While its phase is `AWAITING_AUTO_LAUNCH`, arrange Auto Launch slowly
with the visual focus evidence loop above. Once Auto Launch is observed, the
workflow first requires a `KNOWN` speed of at least 15 to prove that Auto
Launch actually moved the ship. It then requires the Auto Launch prompt to be
absent for five samples, Mass Lock to remain `ON`, and two `KNOWN` speed samples
at or below 10 within the bounded confirmation window. When the finite
classifier returns `UNKNOWN` only because constrained confidence is below
`0.55`, the workflow may instead accept four consecutive observations of the
same raw and constrained `0` through `10` text, each at confidence `0.40` or
higher and margin `0.02` or lower. A changed or non-qualifying candidate clears
that workflow-local temporal count, and events identify which evidence mode
confirmed the handover. Only then does it own
the 100% throttle, Mass Lock OFF, and 0% throttle sequence. Sending 0% is not
completion: the Action enters `VERIFYING_STOP` and requires three consecutive
current `STOPPED` frames backed by the dedicated slashed-zero pixel topology.
A qualified multi-digit OCR observation conflicts with that topology instead
of being overridden. This phase calls only the resident `ship-speed` path; flight prompt
and ship status are explicitly unobserved after the already-confirmed Mass Lock
OFF gate. The workflow-local temporal gate does not weaken the finite
`ship-speed` non-zero OCR classifier's `0.55` single-frame threshold. If zero is not visually
confirmed within 60 samples, the workflow fails explicitly.
Stream events distinguish `commandedThrottle` from the independently observed
speed fields and report `COMPLETED`, `FAILED`, or `CANCELLED`.
Up to five explicitly coded transient WGC capture failures may be skipped and
are emitted as `OBSERVATION_ERROR`; the sixth fails the workflow. Immediately
before the 100% command, the workflow registers a runtime failure compensation
that sends 0% if any later path fails. A successful explicit 0% command clears
that compensation.

## Filesystem information Actions

The following finite Actions explicitly identify Elite Dangerous filesystem
JSON as their sole information source:

- `elite-dangerous/filesystem/status`
- `elite-dangerous/filesystem/cargo`
- `elite-dangerous/filesystem/ship-locker`
- `elite-dangerous/filesystem/backpack`
- `elite-dangerous/filesystem/nav-route`
- `elite-dangerous/filesystem/modules-info`
- `elite-dangerous/filesystem/market`
- `elite-dangerous/filesystem/outfitting`
- `elite-dangerous/filesystem/shipyard`

Each Action performs one bounded query of its same-named Frontier JSON file
under the current user's resolved Windows Saved Games known folder. The Action
ID, not caller input, selects the filename. Results expose source timestamp,
file modification time, observation time, age, update mode, and source-specific
freshness. `ABSENT` means Frontier has not produced that optional file; it does
not authorize another source. Invalid JSON, wrong event discriminators,
reparse points, files changed during the read, oversized files, and an invalid
Saved Games root fail explicitly.

These Actions never consult Player Journal lines, screenshots, OCR, process
memory, network APIs, or previous results. `CURRENT` means only that the
selected filesystem snapshot satisfies its declared age window. It does not
prove visual focus, ship movement, target geometry, docking completion, or any
fact absent from that JSON schema. Event-driven inventory snapshots become
`UNKNOWN`, rather than `STALE`, after their short current window because a
single query cannot prove whether an unchanged older inventory is still
semantically current.
