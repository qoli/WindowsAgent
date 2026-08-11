# Locate the visible Supercruise Escape Vector reticle

This finite composite Action is the explicit ROI-visibility Gate between
Compass coarse navigation and visible-target precision alignment. It reads the
lower forward-HUD OCR band and requires one high-confidence `ESCAPE` region
directly above one high-confidence `VECTOR` region. `SOLID` Compass evidence is
not accepted as a substitute for this Gate.

The reviewed 1920x1080 reference frame placed the blue reticle centre five
pixels left of the paired text's right edge and forty pixels below its bottom
edge. The Action applies that declared geometry and reports displacement from
screen centre. Missing, duplicate, low-confidence, or geometrically invalid
text returns `UNKNOWN` honestly.
