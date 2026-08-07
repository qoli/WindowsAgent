# Read the Elite Dangerous compass

Read the fixed cockpit compass region in the 1920x1080 reference coordinate
space using reference-density sampling. Confirm that the orange compass HUD is
visible, locate the cyan target marker when it is present, and return its
reference-coordinate offset from the fixed compass center. The result also
returns its Euclidean distance in reference pixels, its clockwise screen angle
from straight up, and whether it is inside the fixed circular four-pixel
center zone.

This package is deliberately game-specific. Core maps the reference rectangle
through the centered 16:9 viewport and returns an image with the requested
reference dimensions. The package does not search the screen, invoke
ScreenParser, switch to native sampling, or substitute a model. A valid
compass with no cyan target is a successful observation with `target.detected`
set to false. In that state position, angle, distance, and center-zone
membership are explicitly null. A marker exactly at the center has zero
distance, null angle, and is inside the center zone. Entered/exited transitions
require prior state and therefore belong to a Monitor, not this finite Action.
