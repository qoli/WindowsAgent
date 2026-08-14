# Orbital scale gauge state

Observe the reviewed `640,235,490x133` 1920x1080-reference cockpit region and
classify the orange orbital heading scale as `DETECTED` or `ABSENT`. The
classifier uses current-frame orange hue/saturation evidence, at least six
vertical ticks, reviewed tick spacing, baseline agreement, a taller central
tick, and upper numeric/anchor evidence. A confidence of `0.75` is the fixed
acceptance threshold; confidence is a deterministic evidence score, not a
calibrated probability.

This finite Action never assigns flight semantics or sends input. The owning
Streaming Action may treat `DETECTED` as its near-orbit safety Gate. Capture,
permission, or malformed-image errors fail explicitly and are not converted to
`ABSENT`.
