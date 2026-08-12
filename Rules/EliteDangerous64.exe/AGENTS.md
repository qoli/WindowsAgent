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
active; press Actions on distinct resolved keys remain available, while an
exact physical-key conflict fails before injection. Streaming failure
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
Action over the selected target. Its `controlProfile` defaults to `AUTO`: the
Action requires AVAILABLE Status.json Flags and selects the Supercruise or
normal-space control law itself. Missing or malformed automatic-profile
evidence fails explicitly rather than silently selecting normal space. The
terminal output records the resolved profile and evidence source. By default
it first commands 0% throttle. A
hollow rear marker uses one fixed `PITCH_UP` great-circle hold because live
gravity-well evidence showed that fixed Yaw can orbit a near-centered rear
projection without reaching the front hemisphere. A solid marker farther than
40 reference pixels uses one
leased sustained control while Compass is sampled at a one-second
start-to-start cadence. It releases the hold on hemisphere, axis, or fine-band
transition. A far solid marker with meaningful error on both axes uses the
compound pitch-plus-yaw lease for diagonal movement. Inside 40 pixels it
returns to distance-scaled bounded pulses at the same cadence. Near-center
pulses are 120 ms in normal space and 80 ms in Supercruise. Two measured
no-movement Supercruise samples permit one 240 ms recovery pulse; the retired
1000 ms recovery crossed the center by 34 reference pixels in live testing.
Supercruise sustained-turn release uses a 160 ms reverse brake and requires two
current SOLID contacts. The retired 80 ms brake plus one-frame completion let a
6px transient drift through the rear hemisphere while target distance rose
from 2.71 to 3.04 kLs after thrust was restored.
The 240 ms no-movement recovery is not reverse-braked inside the 16px Gate:
live evidence showed the retired 80 ms recovery brake repeatedly moved a
14–16px result back to 24–25px.
The first pulse-driven alignment-center entry applies a 100 ms
opposite-axis brake before stable verification. Normal-space STATIC alignment
also applies that brake when a 300 ms medium-band pulse first enters its 12px
Gate; live Evidence showed the former unbraked handoff retaining angular
velocity and cycling from 9–11px back to 16–19px. The pre-brake sample is
discarded and only fresh post-brake observations can complete. Events expose
control mode, lease state, sample timing, requested
pulse duration, observed marker movement, distance delta, moving-away trend,
and consecutive no-movement count. Four stationary samples
or five consecutive front samples moving away fail explicitly. Three
consecutive solid samples within the stricter 1.5-pixel alignment radius are required for
completion. Its structured update events are the durable control timeline;
explicit activity events supply the Action OSD. An owning flight workflow may
set `stopBeforeAlign=false` when it already controls throttle. It does not establish Station
target lock, approach the Station, request docking, or participate in the
docking-computer workflow.

`align-station-target mode=TRACK` keeps a HOLLOW target under the same fixed
`PITCH_UP` sustained lease used by ALIGN; one-second 80 ms rear pulses were
live-measured to remain around 22–28px until the command budget was exhausted.
The lease is explicitly released at the tracking-window terminal boundary.
For a front target, Supercruise TRACK uses the calibrated 16-reference-pixel
alignment Gate rather than the generic four-pixel Compass zone. Live approach
evidence showed the four-pixel Gate repeatedly corrected valid 8-12px contacts
back through the rear hemisphere while Station distance stopped improving.
From 16px through the ordinary 40px fine band, Supercruise TRACK uses a 240ms
bounded pulse. The retired 80ms pulse left a 27px marker unchanged for 18
samples, while a sustained fine-band lease crossed the front and rear
hemispheres every second. Above 40px it keeps the sustained lease; inside 16px
it only observes.

`elite-dangerous/flight-prompt-text` is a second finite Action. It captures the
reviewed central prompt as a 400x40 reference-density RGB line and sends it to
the Rule-declared `ocr/w480` resident DirectML worker. Its output is
raw OCR text evidence only.

`elite-dangerous/flight-status` is its finite, pure post-processing Action. It
accepts the complete raw OCR output and combines OCR confidence with reviewed
phrase similarity. A candidate must also meet the explicit `0.60` phrase
similarity floor; high-confidence unrelated OCR remains `UNKNOWN`. It returns `SUPERCRUISE`, `AUTO_LAUNCH`,
`WAITING_IN_QUEUE`, `SLOW_DOWN_FOR_AUTO_DOCK`, `FSD_CHARGING`,
`FSD_THROTTLE_UP_REQUIRED`,
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

`elite-dangerous/advance-toward-station` is an interruptible linear Streaming
Action for bridging the higher model's interaction cadence. It does not measure
travelled distance or world geometry. It reads only the currently displayed
Station target-lock HUD distance through `request-docking-range`. Before
applying a binding-resolved 75% or 100% throttle preset, it requires two
mutually continuous distance observations. The first displayed distance at or
below `stopAtStationDistanceMeters` causes an immediate 0% command before one
stopped confirmation sample. Missing evidence, a discontinuity larger than
1000 metres, two trusted samples moving away, `maxDurationMs`, failure, or
cancellation also invokes 0% compensation. Only `STATION_DISTANCE_REACHED` or
an already-satisfied target completes successfully; use that success before
sequencing `dock-at-station`.

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
`DOCKING_ACTIVE`, `DENIED`, or `UNKNOWN`. A same-frame anchored
`CANCEL DOCKING` row overrides a conflicting denial notification because it is
the persistent post-submit state. A denial without that stronger evidence is
preserved for temporal reconciliation by the owning workflow. A selected
target is never assumed to support docking. Only `FOCUSED`, together with the
independent allowed range Gate, permits a later `SELECT`; the Action itself
never navigates or injects input.

`elite-dangerous/dock-at-station` is a linear Streaming Action. Its range Gate
watches indefinitely and builds a temporal distance trend: two readings within
`1000m` establish continuity, a larger one-frame jump is rejected, and two
mutually continuous readings can rebase the track. Admission requires at least
three trusted trend samples followed by two accepted `ALLOWED` samples;
`UNKNOWN` remains a visible waiting state. Three trusted `DENIED` samples
invoke `advance-toward-station` once with a 7000m stop target; the child owns
its bounded movement and 0% compensation, after which the parent rebuilds the
distance trend from current frames. An already allowed distance does not
invoke the child. After its one-time range admission
and verified docking request, monitoring uses explicit
`action.try_call` results. A failed child observation is written as
`OBSERVATION_ERROR`, does not advance the prompt-disappearance or Landing Gear
Gates, and fails the workflow after three consecutive errors. It never changes
capture or state providers. A single denial notification is non-terminal:
two later `CANCEL DOCKING` observations override it, while two returned Request
Docking observations confirm rejection without resubmitting.

`elite-dangerous/ui-control` is a finite slow-interaction primitive. A
supervising model chooses exactly one logical `FOCUS_LEFT_PANEL`,
`OPEN_GALAXY_MAP`, `NEXT_PANEL`, `PREVIOUS_PANEL`, `UP`, `DOWN`, `LEFT`,
`RIGHT`, `SELECT`, or `BACK` after
inspecting a fresh screenshot.
`FOCUS_LEFT_PANEL` resolves the dedicated Frontier control; `LEFT` remains
in-panel navigation. `NEXT_PANEL` and `PREVIOUS_PANEL` resolve the dedicated
Frontier panel-cycle controls and must not be replaced with `LEFT` or `RIGHT`.
`OPEN_GALAXY_MAP` resolves the dedicated Frontier map control and proves only
key injection, not a plotted route.
The runtime resolves that logical control from the game's active binding preset, then uses the
game-neutral Windows scan-code input driver; never assume Space or any other
fixed physical key. Successful output includes the binding source, backend,
scan code, extended-key flag, and configured hold time.

`elite-dangerous/text-entry-key` is the finite single-key text primitive for a
model-confirmed active game field. It accepts one allowlisted ASCII letter,
digit, Space, Backspace, or Enter and uses the same foreground-revalidated
scan-code input driver. It does not accept a string, clipboard payload, chord,
or arbitrary key name. Use a fresh frame to establish field focus and verify
the resulting text or transition; successful injection alone is not input
acceptance or route creation.

`elite-dangerous/pointer-click` is the finite mouse primitive for game UI that
has no Frontier keyboard-focus route. It accepts one point in the centered
1920x1080 reference coordinate space, maps it to the current primary display,
and emits one left click after foreground revalidation. Use a fresh frame to
identify the control and another fresh frame to verify the resulting focus or
transition. Its output reports both reference and native mapped coordinates;
successful injection alone is never click acceptance.

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

`elite-dangerous/cockpit-hud-presence` is a finite reference-density pixel
observation over the reviewed right ship-hologram HUD region. It returns only `PRESENT` or
`ABSENT` with separate orange and FSD-charge-cyan pixel counts. Neither state is a scene or transition
claim. `elite-dangerous/hyperspace-state` combines that current sample with
the central flight-prompt OCR and reports `FSD_CHARGING`,
`ALIGNMENT_REQUIRED`, `COCKPIT_PRESENT`, or `COCKPIT_ABSENT`. It does not read
Journal, Status, NavRoute, command history, or prior observations.

`elite-dangerous/hyperspace-jump-to-system` is the reusable one-hop Streaming
Action. It verifies or acquires one exact Navigation System target unless the
caller explicitly supplies `targetLockConfirmed=true`. The multi-System route
owner may do so only after the same Status snapshot's Destination name and
SystemAddress exactly match the frozen hop; this avoids reopening Navigation
after the game has already auto-selected the plotted route's next jump. It
coarsely aligns through Compass, runs the current-frame stellar obstruction
Gate before and after visible-target fine alignment, and permits at most two
trend-guided Supercruise escapes when the destination projects through the
local star. Only a `CLEAR` target line may reach `hyperspace-control`.
It then requires charging before stable cockpit absence, and sends 0% on the first
returning cockpit frame after transit has been established. Stable cockpit and
Supercruise HUD evidence then complete the child at
`ARRIVED_IN_SUPERCRUISE`. It does not read a route, choose another hop, or
enter the Station workflow.

`elite-dangerous/hyperspace-target-occlusion` is the finite directional CV Gate
over five sparse horizontal strips in the upper 1680 by 900 forward view. The
5 by 5 stellar occupancy grid excludes the lower cockpit reflection while
retaining upper-screen stellar obstruction. `safeToCharge` is stricter than
`CLEAR`: total sampled stellar coverage must be at most 0.5%, and every cell at
most 2%. A symmetric or full-frame body returns no recommended control rather
than inventing a direction.

`elite-dangerous/clear-hyperspace-occlusion` is its interruptible streaming
owner and owns the complete local realtime escape loop. It stops first,
records forward-view CV only as diagnostic context, and never uses CV to
calculate the escape angle. Landing Gear and Cargo Scoop must be visually OFF;
the latest AVAILABLE `Status.json` baseline must show normal-space idle with
Mass Lock OFF; and `ship-heat` must return three known readings at or below 60%
before activation. Since Status is persistent event state rather than a
heartbeat, a newer source timestamp is mandatory after the Supercruise command
before its flags may advance the workflow.
It uses at most eight short 0%-throttle Supercruise probes to discover and
prealign the Escape Vector. Up to sixteen charging Compass samples at 137 ms
cadence deliberately walk across the flashing marker; two consistent SOLID or
HOLLOW observations are required. An absent-to-detected transition,
presentation change, or at least eight pixels of Manhattan offset movement
confirms charge ownership. OCR-confirmed `ALIGN WITH ESCAPE VECTOR` is optional
strong evidence because live prompts often remain `CHARGING`.

Each cancelled probe yields one Action-local snapshot that authorizes exactly
one attitude segment and then expires. HOLLOW is only a rear-hemisphere
topology signal, so it uses a fixed 3000 ms `PITCH_UP` segment rather than its
unreliable signed offset. SOLID uses 3000/1800/700/200 ms distance bands and
reverses a repeated control when a fresh probe proves the distance worsened.
Heat must cool to three readings at or below 60% before another probe.

SOLID proves only that the target is in the front hemisphere; it never proves
that the visible Escape Vector lies inside the OCR ROI. While the same probe
charge is active, `escape-vector-visible-position` is the independent ROI
Gate. `UNKNOWN` cancels the probe and permits one Compass-derived segment.
Only two consecutive geometrically consistent `DETECTED` observations inside
a three-sample window hand control to heat-protected `align-visible-target`
with its faster Escape Vector profile and bounded active-charge heat policy.
After a
known reading no higher than 60%, charge-obscured UNKNOWN heat is tolerated for
at most four seconds while the visible Escape Vector remains the control
source; known heat at 75% still fails immediately. After two stable visible confirmations, the
Action preserves that charge, commands 100%, stops reading Compass and visible
reticle evidence, and waits for the FSD entry countdown.

The completed output reports prealignment probe, turn, flashing-Compass miss,
visible-handoff, prealignment elapsed, and total elapsed counters. A repeated
near-center SOLID snapshot with less than one reference pixel of measured
improvement receives a 600 ms recovery segment rather than another ineffective
300 ms segment.

Before the visible ROI is acquired, known heat at 75% or three consecutive UNKNOWN heat
observations cancel formal charge. During the already-aligned countdown, the
transient heat wall may hide OCR and reticle evidence, so UNKNOWN heat is
logged without cancellation and only a known reading at 160% cancels. Every
slow heat OCR return is followed by a fresh Status read; a confirmed
Supercruise transition wins the race before cancellation. Mass Lock and
hyperspace-charge flags remain immediate failures. Failure or cancellation
owns STOP-hold, one verified Supercruise-toggle cancellation, and 0% throttle
compensation locally. After current Status confirms Supercruise, toggle
compensation is removed; the Action follows the aligned escape for 30 seconds
and completes at 0% with current `CLEAR` and Supercruise evidence. The parent
must then restore the original hyperspace destination.

`elite-dangerous/ship-heat` is the finite visual charge-start Gate. Its OCR
front end reads the fixed two- or three-digit cockpit heat percentage with
bounded vertical/right margin for HUD inertia while excluding the animated red
heat icon to the left. Its digits-only decoder and pure classifier return
`KNOWN` only for a sufficiently
confident value from 0 through 250 whose constrained and raw readings do not
materially disagree. `UNKNOWN` must delay or reject initial charging. After
capture optimization, live evidence measured complete heat calls at about
0.25 seconds, so the stellar escape Action uses it on every Compass cycle.

`elite-dangerous/nav-route-plan` is the pure semantic boundary over the RAW
`filesystem/nav-route` result. It validates every plotted System entry, the
exact expected final System, unique positive SystemAddress values, and the
caller-owned jump limit, then returns one frozen ordered route identity.
`NavRouteClear`, missing or malformed entries, destination mismatch, and
excessive hop counts fail explicitly. Source age is evidence, not automatic
route invalidation; an owning workflow must compare the route identity again.

`elite-dangerous/multi-system-transit` is the interruptible route owner. It
freezes one game-plotted NavRoute, optionally delegates a docked departure,
then invokes one `hyperspace-jump-to-system` child per ordered hop. Before each
hop and after each arrival it requires a CURRENT `Status.json` snapshot with a
new timestamp and numeric FuelMain plus an explicit minimum fuel reserve. The
Status snapshot does not expose a temperature field, so this Action does not
claim a temperature Gate. It re-reads and compares the route
identity after every hop. Completion means every frozen hop was consumed and
the final System was reached in confirmed Supercruise at 0%; it does not select
a Station or dock. Route mutation, unavailable safety evidence, child failure,
or cancellation terminates with registered 0% compensation.

`elite-dangerous/inter-system-transit-to-station` is the parent single-hop
Streaming Action. It may delegate a docked start to `leave-station`, then
delegates its one System transition to `hyperspace-jump-to-system`. The parent
still requires two exact destination-System target-text observations before it
enters the Station phase. The destination Station
must already be present in the current visible Navigation list; stale filters
fail as a missing target rather than triggering blind icon-menu input. A hyperspace exit is
already Supercruise, so the workflow locks the exact Station and resumes
`supercruise-assist-to-destination` with `supercruiseConfirmed=true` before
delegating to `dock-at-station`. Any missing Gate or child failure terminates
with 0% compensation. The final phase is `VISUAL_CONFIRMATION_REQUIRED`.

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
and the ship's `Auto Throttle` Assist setting. It aligns a Station as a STATIC
target through Compass only: the strict NORMAL_SPACE profile owns pre-entry
alignment, and the SUPERCRUISE_ASSIST profile owns later correction after
entry. It does not invoke `align-visible-target` or replace an out-of-band OCR
label with a blind search. It first enters Supercruise manually, then commands
minimum Supercruise throttle before opening the locked target's Navigation
detail. Two OCR frames must identify the non-orbit
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

`elite-dangerous/station-service-focus` is the finite visual primitive for the
four docked-cockpit service tiles. It reads the row once at reference density,
converts each tile interior to grayscale luminance, and returns `REFUEL`,
`REPAIR`, `RESTOCK`, `LAYER_SWITCH`, or evidence-preserving `UNKNOWN`. It uses
relative brightness plus an absolute floor because unavailable grey tiles can
still receive the game's bright keyboard-focus fill. It never interprets
service availability or remembers a prior focus.

`elite-dangerous/commodity-market-header-text-regions` and
`elite-dangerous/commodity-market-text-regions` are bounded resident PP-OCR
primitives for the open Commodity Market. The first owns the title, Station,
and BUY/SELL mode header; the second owns the visible commodity list and trade
dialog. They remain separate captures so neither request exceeds the OCR
runtime pixel limit. `elite-dangerous/trade-visible-commodity` is an
interruptible linear Streaming Action for one exact row already visible in the
caller's already-selected BUY or SELL tab. It requires two adjacent current
header/list cycles, uses only the exact row box from the list capture for the
click, confirms the matching trade dialog twice, and treats a newer exact
`Cargo.json` count delta as the transaction postcondition. It neither opens
Starport Services nor changes tabs or scrolls; those are explicit caller
preconditions. On success and failure it uses
`elite-dangerous/exit-commodity-market`, which spaces two binding-resolved
`BACK` inputs across the Commodity Market and Starport Services transitions;
failure cleanup allows one extra spaced `BACK` when a trade dialog may still
be open.
The successful path additionally requires the Commodity Market header to
remain absent twice. That proves market exit, not the exact resulting cockpit
screen, so goal-layer confirmation remains a fresh supervising-model capture.
The baseline
Cargo event snapshot may report `UNKNOWN` freshness
when inventory has not changed recently; completion still requires a different
Cargo source timestamp and the exact requested count delta.

`elite-dangerous/docked-cockpit-menu-text-regions` is the bounded raw OCR
primitive for the three centered docked-menu labels. The interruptible linear
`elite-dangerous/open-commodity-market` Action owns the missing transition from
that menu to an exact Station's Commodity Market in caller-selected BUY or
SELL mode. It uses clamped docked-menu navigation, the Rule-owned Market tile,
and two current header confirmations before returning with the market still
open. Input commands or tile clicks are never mode evidence. Use it before
`trade-visible-commodity`; do not reproduce its UI path through high-level
primitive calls.
Quantity changes are sent at 60 ms intervals, while progress events are
coalesced at the first, each twenty-five, and final step so a full cargo load
does not flood the durable journal.

`elite-dangerous/prepare-auto-launch` is a finite composite Action for the
visible docked cockpit menu. Four `DOWN` inputs clamp focus at `DISEMBARK` and
three `UP` inputs enter the service row at its game-remembered horizontal
position. Two consistent `station-service-focus` observations establish the
current tile; the Action computes the minimum cyclic `RIGHT` count to Refuel,
visually confirms Refuel and Repair before their safe purchase attempts, then
returns to the `AUTO LAUNCH` row. `activateAutoLaunch=false` is a safety-test
mode that stops before the final `SELECT`; only `true` sends it. Unknown,
ambiguous, inconsistent, or contradicted focus evidence fails explicitly
without reverting to the retired fixed sequence.

`elite-dangerous/leave-station` is an interruptible linear Streaming Action.
Invoke it with `{"stationConfirmed":true}` only after high-level visual
confirmation that the docked cockpit menu containing `STARPORT SERVICES`,
`AUTO LAUNCH`, and `DISEMBARK` is visible, then follow the returned NDJSON watch
URL. It first calls `prepare-auto-launch` with `activateAutoLaunch=true`; it no longer asks the supervising
model to navigate the menu. Once Auto Launch is observed, the
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
that sends 0% if any later path fails. The compensation is critical and has an
independent timeout, so later panel cleanup cannot consume its execution budget.
A successful explicit 0% command clears that compensation.

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
