# Ship heat text

This fixed forward-cockpit reference ROI sends only the displayed ship heat
digits to the resident constrained w480 OCR worker. Live 4K evidence showed
that the previous wider crop included the red heat icon, a ghosted zero, and
the radar arc, causing a visible `53%` to collapse to a single digit. The
55x50 reference crop retained bounded cockpit HUD inertia, but live normal-space
evidence exposed the opposite truncation failure: a clear `24%` was cut through
the percent glyph and both decoders returned `248`. The current 70x35 crop
retains the complete percent glyph while removing the lower radar arc from the
recognition plane. Its left boundary still excludes the heat icon and ghosted
zero. The percent sign is outside the digit constraint but remains available to
the raw decoder as the classifier's format terminator.
Before resident OCR, this Action applies its manifest-declared generic
`rgb-threshold` filter. The Rule-owned thresholds retain orange HUD strokes and
replace white or cyan radar overlays with black pixels. Evidence records the
filter contract and filtered-pixel count; other OCR Actions without a filter
retain their original RGB input unchanged.
RAW OCR evidence is not a heat classification.
