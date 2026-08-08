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
primitive. It injects exactly one 40 ms `PITCH_UP`, `PITCH_DOWN`, `YAW_LEFT`,
`YAW_RIGHT`, `ROLL_LEFT`, or `ROLL_RIGHT` press from the active Frontier preset.
It proves key injection only, not attitude movement.

`elite-dangerous/align-station-target` is an interruptible linear Streaming
Action over the selected target. It first commands 0% throttle, drives a hollow
rear marker away from center until it becomes solid, then drives the solid
marker toward center with one pitch/yaw pulse and one fresh Compass observation
at a time. Three consecutive solid samples in the four-pixel center zone are
required for completion. Its structured update events are the durable control
timeline; explicit activity events supply the Action OSD. It does not establish
Station target lock, approach the Station, request docking, or participate in
the docking-computer workflow.

`elite-dangerous/flight-prompt-text` is a second finite Action. It captures the
reviewed central prompt as a 400x40 reference-density RGB line and sends it to
the Rule-declared `ocr/w480` resident DirectML worker. Its output is
raw OCR text evidence only.

`elite-dangerous/flight-status` is its finite, pure post-processing Action. It
accepts the complete raw OCR output and combines OCR confidence with reviewed
phrase similarity. A candidate must also meet the explicit `0.60` phrase
similarity floor; high-confidence unrelated OCR remains `UNKNOWN`. It returns `SUPERCRUISE`, `AUTO_LAUNCH`,
`WAITING_IN_QUEUE`, `SLOW_DOWN_FOR_AUTO_DOCK`, `FSD_CHARGING`,
`FSD_ALIGNMENT_REQUIRED`, `AUTO_DOCK`, or evidence-preserving `UNKNOWN`. It
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

`elite-dangerous/request-docking-range` is a finite composite Gate Action. It
captures the fixed lower-left Target distance at reference density through the
internal `request-docking-distance-text` OCR Action, then calls the pure
`request-docking-range-classifier`. It returns `ALLOWED` only when exactly one
current displayed distance is recognized with sufficient confidence and is
strictly below `7500m`; `7.50km` is `DENIED`. Missing, malformed, low-confidence,
or ambiguous evidence is `UNKNOWN`, never a prior-frame or inferred value. Run
it in the settled forward cockpit view before opening the Target panel. The
Action is not registrable and never performs the docking request itself.

`elite-dangerous/request-docking-availability` is a finite composite visual
Gate for the currently selected Contacts target. Its raw text-regions Action
scans a broad action area; PP-OCR dynamically locates each quadrilateral and
rectifies that line before recognition, so a shifted cockpit view is not
interpreted through a fixed action-row crop. The pure classifier distinguishes
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

`elite-dangerous/contacts-tab-state` is a finite same-frame observation Action.
It scans only the fixed filled-highlight region around the `CONTACTS` tab and
returns `SELECTED`, `NOT_SELECTED`, `ABSENT`, or `UNKNOWN`. It does not OCR the
header or identify the other active tab. The icon-only `SYSTEM` summary Tag to
the left of `NAVIGATION` is not one of the four selectable tabs.

`elite-dangerous/select-contacts-panel` is an interruptible linear Streaming
Action. It opens a stably absent Target panel at most once, invokes only the
dedicated `NEXT_PANEL` control, and verifies every transition with two current
CONTACTS-region pixel scans. The four tabs cycle as `NAVIGATION`,
`TRANSACTIONS`, `CONTACTS`, `TARGET`, then back to `NAVIGATION`. It completes
only after `CONTACTS` is confirmed twice. Its scope ends before selecting a
contact, focusing `REQUEST DOCKING`, or requesting docking. Follow its returned
watch URL; unknown evidence or failure to reach Contacts within three cycles
terminates the Action.

`elite-dangerous/select-station-target` is a separate interruptible linear
Streaming Action. It is not a child phase of `dock-at-station`. The caller must
provide the exact Station name. In CONTACTS, a filled row proves keyboard focus
only; angle brackets around the recognized row text, for example
`< MOONGLOW CITY >`, are the direct Station target-lock evidence. The Action
sends `SELECT` at most once and only after two current OCR observations prove
the named visible row is focused. It then requires two current angle-bracketed
observations before reporting `ACQUIRED`; an already bracketed row reports
`EXISTING` without input. A missing visible target, ambiguous OCR, or unknown
focus fails explicitly and never falls back to the first CONTACTS row. The
Action restores the forward view only when it opened the left panel itself.

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

`elite-dangerous/set-throttle` is deterministic. It accepts only `-100`, `0`, or `100`,
resolves the corresponding logical throttle control from the active preset on
each invocation, and reports the exact resolved key and injection evidence.

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
current frames where both OCR candidates are exactly `0`, constrained
confidence is at least `0.45`, the raw-versus-constrained margin is at most
`0.02`. This phase calls only the resident `ship-speed` OCR path; flight prompt
and ship status are explicitly unobserved after the already-confirmed Mass Lock
OFF gate. The workflow-local temporal gate does not weaken the finite
`ship-speed` classifier's `0.55` single-frame threshold. If zero is not visually
confirmed within 60 samples, the workflow fails explicitly.
Stream events distinguish `commandedThrottle` from the independently observed
speed fields and report `COMPLETED`, `FAILED`, or `CANCELLED`.
Up to five explicitly coded transient WGC capture failures may be skipped and
are emitted as `OBSERVATION_ERROR`; the sixth fails the workflow. Immediately
before the 100% command, the workflow registers a runtime failure compensation
that sends 0% if any later path fails. A successful explicit 0% command clears
that compensation.
