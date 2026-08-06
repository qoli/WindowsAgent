# Read the Elite Dangerous compass

Read the fixed cockpit compass region for the reviewed 3840x2160 primary
display profile. Confirm that the orange compass HUD is visible, locate the
cyan target marker when it is present, and return its offset from the fixed
compass center.

This package is deliberately game-specific. It does not resize, search the
screen, invoke ScreenParser, or substitute a model when the fixed profile is
not available. A valid compass with no cyan target is a successful observation
with `target.detected` set to false.
