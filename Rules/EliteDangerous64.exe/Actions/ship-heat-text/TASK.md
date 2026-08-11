# Ship heat text

This fixed forward-cockpit reference ROI sends only the displayed ship heat
digits to the resident constrained w480 OCR worker. Live 4K evidence showed
that the previous wider crop included the red heat icon, a ghosted zero, and
the radar arc, causing a visible `53%` to collapse to a single digit. The
55x50 reference crop retains bounded cockpit HUD inertia while excluding those
high-contrast distractors and the radar's moving vertical range line, which
live Supercruise evidence showed could concatenate a visible `23%` into
`231`/`238`. The percent sign is outside the digit constraint.
RAW OCR evidence is not a heat classification.
