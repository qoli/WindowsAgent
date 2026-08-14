# Elite Dangerous Supercruise target position

This finite composite Action uses PP-OCR first to propose bounded spatial
candidates, then fuses a local CV focus-frame measurement with current HUD
layout and identity evidence. The default `FULL` profile uses one tall central,
two lower, symmetric upper-left and upper-right bands, and symmetric middle-left
and middle-right gap bands. OCR name matching and spatial de-duplication happen
before local CV; only text-compatible candidates receive a fresh 140×140 local
enhancement and 3/4-annulus measurement. If no text-compatible candidate exists,
the Action returns `TARGET_TEXT_NOT_FOUND` without spending local CV work on
unrelated HUD text. OCR never authorizes a control coordinate or decides which
shape is viable by itself.

The explicit `LOS_DIRECTION` profile is available only to the separately
confirmed `MOVE TO OBTAIN LINE OF SIGHT TO TARGET` workflow. It scans the lower
band, then lower-wide band, followed by the normal independent lower-left
identity observation. It does not silently expand to the other five bands.
This bounded ordering prevents a residual closing target from changing scale
for the duration of seven sequential OCR calls. The caller also declares the
reticle evidence policy passed unchanged to every local CV candidate.
The central band covers reference `y=80..400`: live Supercruise evidence placed
an already Compass-aligned planetary marker around `y=185`, above the retired
`y=240..400` strip. Its 800 by 320 shape retains the same 256k reference-pixel
budget. A candidate must
pass both OCR confidence Gates and then match the normalized requested name
exactly or by one substitution, insertion, or deletion. This bounded tolerance
handles live repeated-character loss such as `LT 11244` for `LTT 11244`; short
tokens and two-or-more-edit candidates remain rejected. In the reviewed
Supercruise HUD the marker is immediately left of and below that label. A live
4K frame placed the OCR label left edge at reference `x=988.73` and its centre
at `y=537.5`; the orange destination ring measured `x=960`, `y=550`. The
Action therefore derives the marker 30 reference pixels left of the label and
12.5 pixels below its centre, then reports its displacement from the 1920x1080
screen centre. Matches at a band
boundary are de-duplicated when their reference boxes agree within 16 pixels;
live overlapping lower bands placed the same label 8.76 pixels apart. Elite
also repeats the selected destination in the lower-left
information panel. When distinct same-name candidates remain, each receives an
independent local CV measurement. The Action selects by fused focus-frame
confidence only when the leader exceeds the runner-up by 30 permille. A
nearest-screen heuristic no longer resolves ambiguity. Missing, ambiguous, or
low-confidence evidence returns `UNKNOWN`; Compass evidence is not reused.

Some Station labels near the lower cockpit edge are split into two lines and
partly occluded by the dashboard. For an exact two-word `targetName`, the
Action also accepts one same-frame two-line candidate only when both OCR boxes
pass the normal confidence Gates, their left edges differ by at most 16
reference pixels, their vertical centres are 12 to 36 pixels apart in
top-to-bottom order, and each normalized line contains at least four characters
matching the corresponding requested word prefix with at most one edit. This
recognizes reviewed `CREL` / `STAN` evidence for `CREON'S STANDING` without
accepting arbitrary partial text or dropping target identity. The combined
two-line box, not either fragment alone, owns marker geometry. Other word
counts and unmatched fragments remain `UNKNOWN`.

The upper-right band covers reference x=1120–1920 and y=80–400. It is required
by live Supercruise Assist evidence where the selected Station label remained
near the right HUD edge while `ALIGN WITH TARGET DESTINATION` was visible and
all centre/lower bands were empty. It remains an explicit observation;
target matching and ambiguity rules are unchanged.

The middle-right band covers reference x=960–1920 and y=360–626. Live
`AASGANANU` evidence placed a selected target's partly occluded label near
reference x=1341, y=398, between the upper-right and lower-wide bands. The band
returned `SGANANU` at 0.815 detector and 0.994 recognizer confidence. That
single-token suffix is accepted only when the same invocation independently
confirms the complete `AASGANANU` identity in the lower-left HUD and the local
3/4-annulus measurement passes all shape and layout Gates. It is not a general
partial-text match.

Some System names begin with a short numeric token, such as `47 ARIETIS`.
When a cockpit pillar removes that token from the forward label, the remaining
complete name word may provide a spatial proposal only when the same invocation
independently reads the complete requested identity from the lower-left HUD.
The omitted token must be one to three digits, the surviving word must pass the
normal exact-or-one-edit rule, and the local 3/4-annulus plus layout confidence
Gates remain mandatory. A bare name word without current exact identity or a
non-numeric omitted prefix remains `UNKNOWN`.

The middle-left band covers reference x=0–960 and y=360–626. Houssay Ring
evidence placed its selected label near reference x=35, y=474, inside the
previous x=0–800, y=400–480 gap between upper-left and lower-wide OCR. This is
an OCR proposal coverage correction only: the projected local 3/4-annulus must
still pass the same shape, layout, and ambiguity Gates.

The symmetric upper-left band covers reference x=0–800 and y=80–400. Live
post-jump evidence placed `LP 298-42` at reference x=258, y=390 while the
Compass remained centred; all central, lower, and upper-right bands were empty.
The new band uses the same resident PP-OCR provider and 256k reference-pixel
budget. It does not change target matching or permit a different evidence
source.

Long multiword System names may be rendered as two stacked lines, for example
`TASCHETER` above `SECTOR TE-Q A5-1`. The Action combines such boxes only from
the same OCR band and frame, with the same 16-reference-pixel left-edge and
12–36-reference-pixel line-spacing bounds, and only when the complete normalized
concatenation matches the requested target exactly or by the normal one-edit
rule. Partial names and unrelated stacked HUD text remain `UNKNOWN`.

A cockpit pillar can also occlude only the middle of a two-word Station proper
name while leaving one OCR line, as in the reviewed `SW STATION` evidence for
`SHAW STATION`. That shape is accepted only when there are exactly two tokens,
the proper-name fragment contains at least two ordered characters matching both
the first and last expected proper-name characters, and the complete type word
passes the normal exact-or-one-edit matcher. A bare `STATION`, a mismatched
endpoint, a reordered fragment, or a partial type word remains `UNKNOWN`.

If the pillar leaves only one ordered character of the proper name, the
position fragment is usable only when a separate current-invocation lower-left HUD
observation contains the complete requested target name at the normal
confidence Gates and the position line still contains exactly two tokens with
the complete type word. Thus reviewed `W STATION` can locate a target only in
the same invocation that independently confirms `SHAW STATION`; neither observation
alone is enough. This cross-check never substitutes a prior lock or cached
identity.

The same pillar can split one horizontal label into two OCR boxes, as in
reviewed `SHAW` and `TATION`. For a two-word target these boxes are combined
only in requested-word order, with both tokens passing the normal
exact-or-one-edit matcher, vertical centres within 20 pixels, and a horizontal
separation from 20 pixels of detector-box overlap through a 120-pixel gap. The first box remains the label's
left edge for marker geometry. Reversed, vertically separated, or unrelated
boxes are not combined.
When exact current-invocation lower-left identity is independently confirmed, the
proper-name side may be a three-character exact prefix such as `SHA`; the type
side must still pass the complete exact-or-one-edit rule and the same spatial
constraints. Because pillar clipping lowers the detector's box score for that
tiny prefix, this identity-corroborated path alone accepts detection confidence
0.55 with recognition confidence 0.90; every normal candidate retains the
0.70/0.75 Gates, and final position still requires the independent ring CV.
Under the same current-invocation identity corroboration, the type side may be an
exact suffix of at least four characters such as `ATION`; shorter or
non-suffix fragments remain unusable.

The detector can alternatively fuse both pillar sides into one malformed box,
as in reviewed `SHAViTATION`. Such a box is positional evidence only when the
same invocation independently confirms the complete requested identity, the fused
text preserves the first three requested proper-name characters exactly, and
some suffix passes the normal exact-or-one-edit match for the complete type
word (`TATION` versus `STATION`). Without all three conditions it remains
`UNKNOWN`; this is not a general edit-distance relaxation.

Text geometry is only a bounded local search hint. For every text-compatible
candidate the Action calls `supercruise-visible-reticle-position` while no
attitude command is active to locate the actual target ring in a fresh 140×140
local region. The local detector first enhances `HSV_ORANGE` and
`ORANGE_OPPONENT` evidence, then scores the fifty-four-bin three-quarter arc,
the intentional right-side label sector, radial contrast, and centre
uniqueness. Only a shape confidence of at least 520 permille may enter fusion.
The candidate then receives a layout score from the measured ring-to-label
horizontal and vertical relationship and bounded OCR confidence. Final
confidence is `60% shape + 25% layout + 15% text`; layout must be at least 400
and the fused result at least 650 permille. Thus high OCR confidence cannot
rescue a star edge or another orange object whose focus-frame geometry fails.
The output preserves all confidence components, gaps, CV capture time, selected
evidence plane, angular topology, and solid/dashed presentation. A missing or
ambiguous shape returns `UNKNOWN`; the OCR-derived hint is never used directly
for steering. The composite does not claim that the sequential OCR and reticle
captures are the same frame. `align-visible-target` owns the subsequent local
tracking loop and periodically reacquires exact identity.
If the pillar leaves only one position box beginning with the first three
proper-name characters, such as `SHAW S`, it may provide that local search
hint only after the lower-left band independently confirms the complete exact
target identity. The hint itself is never a detected position: the orange-ring
CV must still succeed in the same invocation.
