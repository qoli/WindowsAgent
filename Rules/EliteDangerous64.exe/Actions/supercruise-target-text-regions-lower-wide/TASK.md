# Wide lower Supercruise target text regions

Detect current-frame target labels across reference x=0–1600 and y=480–640.
This 256000-pixel band complements the center-biased lower region when Compass
coarse evidence follows a different nearby HUD marker and the requested
destination remains near a horizontal edge. The requested target name is still
selected only by `supercruise-target-position`; this raw Action does not infer
identity or choose the nearest visible label.
