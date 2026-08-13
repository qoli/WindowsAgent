# Read the Elite Dangerous Compass

Read the fixed cockpit Compass crop at `(682, 771, 96, 96)` in the centered
1920x1080 reference coordinate space. The Action uses an explicit two-stage
sampling contract. It first captures the 96x96 `reference` image. That fast
path completes only when strict and opponent routes agree on the same non-NONE
class and both confidences are at least `0.5`. Otherwise it captures the same
region once more with `native` sampling, so a 4K frame retains a 192x192
physical crop. Infrastructure, foreground, protocol, shape, and pixel-budget
errors remain terminal and never authorize the second path.

The fallback is intentionally visible. `samplingPath` is
`REFERENCE_FAST_PATH` or `NATIVE_FALLBACK`, `fallbackUsed` is explicit, and
`attempts` retains each sampling mode, capture timestamp, dimensions, geometry
Gate, route predictions/confidences, outcome, and transition reason. Reference
`NONE`, weak confidence, route disagreement, or insufficient annular geometry
may escalate; no other condition silently changes sampling or algorithm. This
Action is deliberately Elite-Dangerous-specific; the generic screen API only
maps and captures the declared region.

The Action first confirms the orange Compass itself. It collapses the native
orange mask only for a bounded Hough-style center/radius vote, searches the
reviewed 24–44 reference-pixel radius range, and rejects candidates without
enough angular coverage or with implausible annulus density. The selected
circle center is measured from the current frame. Target offsets, distance,
angle, and center-zone membership therefore use the observed Compass center,
not a permanent `(48,48)` assumption.

Target-marker classification stays at native pixel density. Two independent
responses are evaluated:

- `opponent`: `max(0, min(g,b)-r)` across the reviewed threshold ladder;
- `strict`: cyan chroma requiring `min(g,b) >= 100`, a 12-count advantage over
  red, and bounded green/blue disagreement.

At 1.5x or greater native scale, each response receives one grayscale 3x3
close before thresholding. This is equivalent to closing every binary level
set but avoids repeating morphology for all opponent thresholds. Connected
components are constrained by native area, reference-space size/aspect, and
distance from the observed Compass circle. Fill, circularity, core density,
size quality, response strength, and threshold support produce separate
`SOLID` and `HOLLOW` scores.

The opponent route is primary. Arbitration is explicit and returned in
`target.cascadeMode`:

- `OPPONENT_PRIMARY` — confident opponent result;
- `STRICT_LOW_OPPONENT_CONFIDENCE` — strict replaces a low-confidence opponent
  result;
- `STRICT_RECOVERY` — opponent reports no marker while strict is confident;
- `CLASS_DISAGREEMENT` or `LOW_CONFIDENCE_DISAGREEMENT` — conflicting evidence
  is reported as `UNKNOWN` and must not authorize steering;
- `NO_MARKER` — neither route supplies a usable target.

`routes.strict` and `routes.opponent` retain their predictions, confidence,
scores, and cluster counts. Consumers and streaming logs must preserve this
provenance instead of presenting a fallback or disagreement as an ordinary
marker detection.

The classifier and thresholds come from a manually reviewed 141-frame study
with grouped near-duplicate train/holdout separation. Both native strict and
native opponent routes classified that bounded offline set correctly. The
reference dual-route Gate accepted 78 of the 88 reviewed marker frames and all
78 accepted results were correct; the remaining marker frames and every
reference NONE/disagreement route to native. This is regression evidence, not
a claim of universal or live-game accuracy; fresh Windows acceptance remains
required after deployment.

No ScreenParser, OCR model, cached frame, alternative ROI, or third sampling
mode is used. Missing native orange annular geometry after an authorized
reference escalation fails with `COMPASS_NOT_VISIBLE`. A valid native Compass
with no target is a successful observation with null position fields. A marker
at the observed center has zero distance, null angle, and is inside the
four-reference-pixel center zone.
