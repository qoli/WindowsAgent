# Middle-right Supercruise target text regions

Detect current-frame target labels in reference x=960–1920 and y=360–626.
This fixed PP-OCR band closes the observed gap between the upper-right and
lower HUD bands. In the reviewed live scene, the selected target label was
near reference x=1415, y=420 while its orange focus frame was partially
occluded by the right cockpit pillar.

This raw Action returns OCR proposals and boxes only. It does not infer that a
proposal is the selected target. `supercruise-target-position` projects each
eligible box into a small local reticle ROI, measures the orange 3/4 annulus,
and then fuses shape, layout, and target-text evidence. Missing, ambiguous, or
weak geometric evidence remains `UNKNOWN`.
