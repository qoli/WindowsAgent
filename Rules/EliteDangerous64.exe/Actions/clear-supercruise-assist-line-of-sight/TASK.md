# Clear a Supercruise Assist line-of-sight obstruction

This interruptible linear Streaming Action owns the fast response to
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET`. It does not align the destination.

After two classified `SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` samples, it
commands 0% and installs critical 0% failure compensation. The internal
`supercruise-sphere-direction` observation module must detect the current body
and provide one image-space outward direction. The controller fixes that
direction for the complete turn; it never switches back to destination-reticle
steering while the body obstructs the view.

After every bounded 800 ms attitude pulse, the Action obtains fresh body
geometry. Signed limb clearance must increase. A body exit is accepted only
after a detected body reached a viewport edge and three consecutive valid
current frames contain no accepted sphere. `UNKNOWN`, a single detector miss,
prompt disappearance, or input success cannot prove exit.

Once the body has left the viewport, attitude input stops. The Action commands
100% for exactly 30 seconds, polling current flight status every 500 ms, then
unconditionally commands 0%. It completes only after two positive current OCR
states prove the LOS prompt is absent. `UNKNOWN` resets this Gate and never
counts as absence evidence.

The terminal postcondition is `sphereExitConfirmed=true`, a completed
30,000 ms separation lease, positive prompt-clear evidence, and commanded 0%
throttle. The caller must then perform the separate 4.5 handoff: Compass coarse
alignment, visible focus-frame fine alignment, and only afterward reacquire
Supercruise Assist.
