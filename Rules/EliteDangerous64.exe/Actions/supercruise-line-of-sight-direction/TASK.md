# Suggest a Supercruise Assist line-of-sight bypass direction

This finite composite Action converts one fresh, named Supercruise target
focus-frame observation into one bounded attitude direction. It is a sensor,
not a steering Action.

The child `supercruise-target-position` must independently confirm the exact
requested identity in the current invocation and return a `DASHED`
three-quarter focus frame. This Action explicitly requests the bounded
`LOS_DIRECTION` OCR profile and the `OCCLUSION_AWARE` reticle evidence policy.
Adaptive `HSV_ORANGE` evidence remains preferred; a viable same-current-ROI
`STRICT_RGB` three-quarter annulus may authorize this confirmed LOS workflow
only after both adaptive planes reject. `ORANGE_OPPONENT` remains insufficient
for direction. The existing shape and fused-confidence Gates remain mandatory.
A focus frame less than 48 reference pixels from screen centre cannot
distinguish a bypass side and returns `UNKNOWN`.

For a sufficiently displaced frame, the Action chooses the screen-space pitch,
yaw, or diagonal attitude control that initially turns toward the target's
visible orbital side. The owning controller must keep this direction fixed
through the centre crossing, measure fresh target geometry after every bounded
turn, and use the original `MOVE TO OBTAIN LINE OF SIGHT TO TARGET` prompt as
the completion Gate. It must not recompute the direction after crossing centre
or interpret this output as proof that the body has been cleared.

Missing target geometry, a solid frame, an unapproved evidence winner, weak
confidence, or an ambiguous near-centre frame returns structured `UNKNOWN`.
The exact child reason is preserved when target acquisition fails, so the
streaming owner can distinguish missing text, missing shape, and ambiguity.
No Compass coordinate, cached frame, fixed default direction, or alternative
colour algorithm is substituted.
