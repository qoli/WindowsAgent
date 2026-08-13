# Suggest a Supercruise Assist line-of-sight bypass direction

This finite composite Action converts one fresh, named Supercruise target
focus-frame observation into one bounded attitude direction. It is a sensor,
not a steering Action.

The child `supercruise-target-position` must independently confirm the exact
requested identity in the current invocation and return a `DASHED`
three-quarter focus frame from the `HSV_ORANGE` evidence
plane. The reviewed image-enhancement ablation found that this plane preserves
dim orange/yellow HUD strokes while suppressing magenta bodies. The existing
shape and fused-confidence Gates remain mandatory. A focus frame less than 48
reference pixels from screen centre cannot distinguish a bypass side and
returns `UNKNOWN`.

For a sufficiently displaced frame, the Action chooses the screen-space pitch,
yaw, or diagonal attitude control that initially turns toward the target's
visible orbital side. The owning controller must keep this direction fixed
through the centre crossing, measure fresh target geometry after every bounded
turn, and use the original `MOVE TO OBTAIN LINE OF SIGHT TO TARGET` prompt as
the completion Gate. It must not recompute the direction after crossing centre
or interpret this output as proof that the body has been cleared.

Missing target geometry, a solid frame, a non-HSV evidence winner, weak
confidence, or an ambiguous near-centre frame returns structured `UNKNOWN`.
No Compass coordinate, cached frame, fixed default direction, or alternative
colour algorithm is substituted.
