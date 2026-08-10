# Hyperspace target stellar obstruction

This finite CV Action samples a 1680 by 900 wide forward-flight viewport in the
1920 by 1080 reference space. It sparsely measures tone-mapped high-luminance
pixels for stellar coverage and centroid direction, while reporting warm-orange
pixels only as diagnostics because the HUD and cockpit trim are intentionally
orange. It exposes a 5 by 3 occupancy grid, center and complete-view coverage,
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

This is current-frame image evidence only. It does not identify the body,
claim FSD rejection, inject input, or infer clearance from a previous frame.
