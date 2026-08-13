# Middle-left Supercruise target text regions

Detect current-frame target labels in reference x=0–960 and y=360–626. This
fixed PP-OCR band closes the observed x=0–800, y=400–480 gap between the
upper-left and lower-wide HUD bands. In the Houssay Ring live failure, Compass
completed with 19.105 reference pixels of residual error and the destination
label appeared near reference x=35, y=474.

This raw Action returns OCR proposals and boxes only. It does not infer that a
proposal is the selected target. `supercruise-target-position` projects each
eligible box into a small local reticle ROI, measures the orange 3/4 annulus,
and then fuses shape, layout, and target-text evidence. Missing, ambiguous, or
weak geometric evidence remains `UNKNOWN`.
