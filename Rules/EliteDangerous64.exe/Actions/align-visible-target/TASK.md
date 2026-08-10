# Precisely align a visible Elite Dangerous HUD target

This interruptible linear Streaming Action refines a target that is already
visible in the forward HUD. It takes the selected target name, repeatedly calls
`elite-dangerous/supercruise-target-position`, and applies bounded dominant-axis
Pitch or Yaw pulses until the OCR-derived target marker remains within 12
reference pixels of the 1920x1080 screen centre for three consecutive samples.

This is deliberately separate from `align-station-target`. Compass alignment
handles rear-hemisphere and large-angle navigation; visible-target alignment
handles the tighter on-screen Gate needed by FSD and similar forward-target
workflows. The Action does not select a target or engage FSD. By default it
commands 0% throttle before turning. Unknown target text is tolerated for at
most seven consecutive frames because live Pitch/Yaw motion can temporarily
blur or animate the OCR label; no control input is sent from an UNKNOWN frame,
and the eighth consecutive miss fails explicitly. Only exact deadline failures
receive five bounded retries; other observation failures remain explicit.
