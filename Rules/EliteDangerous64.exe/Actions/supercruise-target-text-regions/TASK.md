# Supercruise target text regions

Detect current-frame target labels in the wide forward-HUD band after Compass
coarse alignment. The 1600 by 160 reference region stays within the resident
PP-OCR runtime's 262144-pixel bound while covering the two live post-Compass
label positions measured near reference y=279 and y=323. The narrower
Contacts-detail ROI excluded the former at its x/y edge.

This raw Action returns boxes and text only. It does not infer a target from
distance text, Compass state, prior frames, or the left-hand selected-target
summary. `supercruise-target-position` must still require exactly one label
matching its requested target name; absent or obscured names remain UNKNOWN.
