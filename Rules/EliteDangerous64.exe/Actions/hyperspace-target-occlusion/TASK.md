# Hyperspace target stellar obstruction

This finite CV Action samples five seven-pixel horizontal strips across the
upper and central 1680 by 900 forward-flight viewport in the 1920 by 1080
reference space. The first strip starts at reference y 20 to close the
reproduced top-edge blind spot where a visibly dominant star was previously
missed. Because that strip extends above the nominal ROI origin, the public
ROI-relative centroid is clamped at its top boundary while direction continues
to use the unclamped sample position. The former lower-cockpit strip is deliberately excluded:
bright cockpit reflections are not stellar evidence. It sparsely measures tone-mapped high-luminance
pixels for stellar coverage and centroid direction, while reporting warm-orange
pixels only as diagnostics because the HUD and cockpit trim are intentionally
orange. It exposes a 5 by 5 occupancy grid, center and complete-view coverage,
stellar centroid, escape vector, and direction confidence. The wide field is
deliberate: moving a nearby star outside a small central ROI is not evidence
that the ship faces away from the stellar thermal zone.

`BLOCKING` means either the central target cell or the complete forward view is
substantially occupied. `PARTIAL` preserves non-trivial stellar presence;
`CLEAR` requires both central and complete-view bright ratios below 0.015. The
recommended attitude control moves the observed stellar centroid farther
toward its current screen edge. When a nearly symmetric or full-frame body
makes that direction unreliable, the Action returns a null control and the
owning streaming workflow must use a bounded probe-and-measure trend instead of
inventing a direction.

Because the sparse sampling deliberately excludes the cockpit, vertical
direction is normalized against the five-strip sampling centroid rather than
the larger ROI midpoint. A uniformly filled stellar frame therefore remains
directionally ambiguous instead of inventing Pitch Down.

`safeToCharge` is a stricter and independent angular gate. It requires total
stellar coverage at or below 0.5% and every sampled grid cell at or below 2%.
Therefore `CLEAR` is not by itself permission to charge: a large star clipped
by the top or side of the forward viewport remains charge-unsafe. This is an
angular image gate only; the streaming owner still checks current heat and
Status evidence.

This is current-frame image evidence only. It does not identify the body,
claim FSD rejection, inject input, or infer clearance from a previous frame.
