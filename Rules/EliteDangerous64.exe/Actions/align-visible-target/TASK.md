# Precisely align a visible Elite Dangerous HUD target

This interruptible linear Streaming Action refines a target that is already
visible in the forward HUD. It first calls
`elite-dangerous/supercruise-target-position` to acquire the exact requested
identity and a confirmed reticle centre while no attitude command is active.
It then feeds each current reticle centre into
`elite-dangerous/supercruise-visible-reticle-position` as the next bounded local
CV hint. Only that fresh tracked centre can authorize the next dominant-axis
Pitch or Yaw pulse. The OCR identity result only establishes identity and a
hint; it never directly authorizes steering. The Action immediately requires a
current-frame local CV confirmation before issuing a pulse. Identity is reacquired after 32 tracked samples,
whenever tracking returns `UNKNOWN`, or whenever the centre leaves the local
tracker's valid hint range. A tracking miss sends no input and the event stream
exposes the transition back to identity acquisition.

This separates slow identity acquisition from fast visual feedback without
weakening target ownership. It does not cache an old coordinate as current
evidence, change capture provider, or silently switch algorithms. The reticle
classifier evaluates the declared strict-RGB, orange-opponent, and HSV-orange
planes on one current 140x140 RGB region and reports the chosen plane, quality,
topology, and capture time in the alignment stream. Completion still requires
the tracked marker to remain within 12 reference pixels of the 1920x1080 screen
centre for three consecutive destination samples.
Destination identity acquisition and local tracking explicitly request the
`HUD_OVERLAY_AWARE` evidence policy. Adaptive orange remains primary; only
when both adaptive planes reject a current frame may a valid strict-RGB
three-quarter ring be selected. Live Obama Reach evidence showed the selected
body and orbit overlay filling every adaptive angular bin while strict RGB
still isolated the SOLID focus ring at `0.830` shape confidence. The selected
plane and policy remain current-frame provenance and neither can rescue missing
target identity or invalid label-to-ring layout.

This is deliberately separate from `align-station-target`. Compass alignment
handles rear-hemisphere and large-angle navigation; visible-target alignment
handles the tighter on-screen Gate needed by FSD and similar forward-target
workflows. The Action does not select a target or engage FSD. By default it
commands 0% throttle before turning. Unknown acquisition or tracking evidence
is tolerated for at most seven consecutive frames because live Pitch/Yaw motion
can temporarily blur the OCR label or reticle; no control input is sent from an
`UNKNOWN` frame, and the eighth consecutive miss fails explicitly. Exact deadline failures and
the specifically identified transient persistent-WGC region-capture failure
each receive at most five skipped observations. WGC retry events retain the
original error code and text and remain infrastructure errors; they are never
converted to target `UNKNOWN`, never authorize steering, and never select a
different capture backend. A sixth such failure is terminal. Other observation
failures remain explicit.

`positionSource=DESTINATION` preserves the selected-destination OCR path.
`positionSource=ESCAPE_VECTOR` instead calls the dedicated
`escape-vector-visible-position` Gate. The latter must actually detect the
two-line blue reticle label; a SOLID Compass marker alone never selects it.

After exact destination identity acquisition, current-frame local tracking
accepts either the reviewed `SOLID` or `DASHED` three-quarter selected-target
reticle presentation. A `DASHED` ring is valid position evidence for a plotted
hyperspace destination; it does not bypass identity, heat, stable-centre,
stellar-obstruction, or caller-owned FSD Gates. `UNKNOWN` still authorizes no
steering.

The Action never searches for an absent target. An UNKNOWN target frame emits
no attitude command, and eight consecutive misses fail explicitly. The owning
workflow must first use Compass or another domain Action to bring the selected
target into the reviewed OCR bands. This keeps rear-hemisphere and large-angle
navigation out of the visible-target fine-alignment loop and prevents an OCR
miss from authorizing blind steering.

The Escape Vector profile is time-sensitive: it samples at 350 ms cadence,
uses 500/300/160 ms correction pulses above 40/20/12 pixels, and accepts two
consecutive within-12-pixel frames. The ordinary destination profile retains
its 750 ms cadence and three confirmations. Destination pulses are capped at
120 ms while local tracking is active. Live Evidence measured a 300 ms Pitch
pulse moving the reticle 50–53 reference pixels, outside the local tracker's
28-pixel candidate span; 120 ms retains a conservative closed-loop margin. The
bounded command budget is 160 pulses within 192 total samples. The sample
budget includes the required periodic identity revalidation, heat checkpoints,
and final passive centre confirmations instead of terminating a
still-converging controller at the former 120-sample boundary. Live
normal-space Obama Reach evidence
showed a correctly identified SOLID focus frame continuously converging from
about 405 to 69 reference pixels while exhausting the former 80-pulse budget;
the larger bound preserves the reviewed micro-pulse gain and all current-frame,
heat, cancellation, and failure-compensation Gates instead of increasing pulse
amplitude. The profile retains the proven 80 ms pulse inside 20 pixels. Live v9
evidence needed eleven 80 ms pulses to
traverse roughly 36 to 13 pixels; the split raises only that inefficient
mid-fine band while preserving the near-centre gain.
Near-centre destination Yaw uses 120 ms while Pitch remains at 80 ms. Live
left/right tests showed 80 ms Yaw repeatedly moving only 0–2 reference pixels
and requiring eleven commands to close a 26-pixel error; the already exercised
120 ms Yaw moved about 3.6 pixels per sample in the adjacent band. Escape
Vector gains remain unchanged.
After at least one destination sample has entered the 12-pixel Gate, at most
two consecutive 12–14 pixel samples are treated as OCR boundary-jitter
observations without sending input. The stable counter is reset, so completion
still requires three consecutive current samples at or below 12 pixels. A
third outside sample resumes ordinary feedback control. Any control command or
UNKNOWN target invalidates the prior Gate entry, so a target that has not
entered the Gate never receives this tolerance. Live vertical evidence showed
two centred samples followed by 13.27 and 12.32 pixel OCR estimates; one-sample
tolerance issued an unnecessary 80 ms Pitch pulse on the second estimate.

`DESTINATION` mode obtains a bounded visual `ship-heat` checkpoint before the
first target observation and refreshes it every 32 target samples. This keeps a
continuously detected same-frame reticle track alive through a full coarse-to-
fine correction when a cockpit pillar temporarily hides the label; a lost
reticle still invalidates the track immediately and forces identity reacquisition.
Known
heat at or above 75% requires a second confirming sample; UNKNOWN or one false
high sample cannot authorize the checkpoint. Intermediate target events report
heat as unobserved (`null`), not as cached current evidence. This avoids rapid
alternation between the constrained w480 worker and the destination
text-regions worker: live Windows evidence reproduced native `0xC0000005`
Agent exits under that alternation, while 44 consecutive text-regions
observations remained stable.
The heat dependency accepts only an explicit raw `%` reading as KNOWN and
preserves missing or low-confidence percent syntax as `UNKNOWN`; a conflicting
digits-only candidate cannot authorize the Gate. The destination checkpoint
uses up to eight same-provider samples with 250 ms between UNKNOWN results to
recover from bounded post-turn HUD inertia without changing ROI, model, or
evidence source. Live Evidence showed five UNKNOWN results exhausted within
0.94 seconds immediately after a 600 ms Yaw input, while the unchanged HUD
then produced ten consecutive explicit `23%` results. The stream includes the
classifier's `heatReason` so future UNKNOWN windows can be attributed without
weakening the Gate. No attitude input is sent until one sample is explicitly
KNOWN and below the heat threshold.

`ESCAPE_VECTOR` mode still calls visual `ship-heat` every alignment cycle
because active FSD charge changes heat quickly and its target position comes
from CV rather than the text-regions worker. Known heat at or above 75% fails
after two confirmations; three consecutive UNKNOWN heat observations also
fail. Events carry the current `heatState` and `heatPercent`. A charging parent
must register FSD-cancel and 0%-throttle failure compensation before invoking
this Action, so a heat failure closes the charge instead of merely stopping
OCR.

`heatPolicy=ESCAPE_VECTOR_CHARGE` is a narrow exception available only with
`positionSource=ESCAPE_VECTOR`. FSD charge animation can temporarily hide the
heat digits even while the blue Escape Vector remains measurable. After a
known reading no higher than 60%, this policy tolerates UNKNOWN heat for at
most four seconds and emits `HEAT_UNKNOWN_ESCAPE_CHARGE_GRACE`; known heat at
75% still fails immediately. The grace expires without renewal and does not
apply to destination alignment.
