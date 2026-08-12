# Precisely align a visible Elite Dangerous HUD target

This interruptible linear Streaming Action refines a target that is already
visible in the forward HUD. It takes the selected target name, repeatedly calls
`elite-dangerous/supercruise-target-position`, and applies bounded dominant-axis
Pitch or Yaw pulses until the OCR-derived target marker remains within 12
reference pixels of the 1920x1080 screen centre for three consecutive samples.
The destination source consumes `supercruise-target-position` v4 geometry: the
marker is 30 reference pixels left of and 12.5 pixels below the matched target
label centre. This calibration is based on the target ring itself rather than
the text box and prevents the controller from stabilizing on the opposite side
of the vertical centre.

This is deliberately separate from `align-station-target`. Compass alignment
handles rear-hemisphere and large-angle navigation; visible-target alignment
handles the tighter on-screen Gate needed by FSD and similar forward-target
workflows. The Action does not select a target or engage FSD. By default it
commands 0% throttle before turning. Unknown target text is tolerated for at
most seven consecutive frames because live Pitch/Yaw motion can temporarily
blur or animate the OCR label; no control input is sent from an UNKNOWN frame,
and the eighth consecutive miss fails explicitly. Only exact deadline failures
receive five bounded retries; other observation failures remain explicit.

`positionSource=DESTINATION` preserves the selected-destination OCR path.
`positionSource=ESCAPE_VECTOR` instead calls the dedicated
`escape-vector-visible-position` Gate. The latter must actually detect the
two-line blue reticle label; a SOLID Compass marker alone never selects it.

The Action never searches for an absent target. An UNKNOWN target frame emits
no attitude command, and eight consecutive misses fail explicitly. The owning
workflow must first use Compass or another domain Action to bring the selected
target into the reviewed OCR bands. This keeps rear-hemisphere and large-angle
navigation out of the visible-target fine-alignment loop and prevents an OCR
miss from authorizing blind steering.

The Escape Vector profile is time-sensitive: it samples at 350 ms cadence,
uses 500/300/160 ms correction pulses above 40/20/12 pixels, and accepts two
consecutive within-12-pixel frames. The ordinary destination profile retains
its 750 ms cadence and three confirmations. It uses 300 ms above 120 pixels,
160 ms through 40–120 pixels, 120 ms through 20–40 pixels, and the proven 80 ms
pulse only inside 20 pixels. Live v9 evidence needed eleven 80 ms pulses to
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
first target observation and refreshes it every eight target samples. Known
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
