# Supercruise visible reticle position

Locate the orange selected-target ring inside one 140×140 reference-pixel
window supplied by the owning target-identity composite. The hint is only a
search-window centre derived from current-frame OCR; it is not returned as the
target position.

The Action evaluates a bounded grid of candidate centres and counts orange HUD
pixels in a 34–58 pixel annulus. A unique score of at least 18 sampled ring
pixels is required. A near-tied candidate returns `UNKNOWN` only when its
centre is at least 20 pixels away, representing a distinct possible ring;
adjacent 4-pixel grid samples are one local peak. Missing evidence remains
`UNKNOWN`. This
local CV deliberately ignores target identity; `supercruise-target-position`
must independently confirm the requested name in the same frame before it may
use this position.

The fixed 140×140 ROI and fixed candidate grid bound computation. The package
has a 32M Starlark step budget because orange HUD text can transiently increase
the extracted point set; exceeding that explicit bound remains a terminal
infrastructure failure rather than domain `UNKNOWN`.
