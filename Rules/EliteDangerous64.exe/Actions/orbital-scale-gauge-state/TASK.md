# Orbital scale gauge state

Observe the reviewed `1120,390,145x330` 1920x1080-reference cockpit region and
classify the right-side orange orbital vertical scale as `DETECTED` or
`ABSENT`. The classifier requires one long vertical spine plus at least three
compact horizontal marks spread across the spine. It deliberately excludes the
always-present horizontal heading scale and rejects the taller coordinate text
below the orbital scale. A confidence of `0.75` is the fixed acceptance
threshold; confidence is a deterministic evidence score, not a calibrated
probability.

This finite Action never assigns flight semantics or sends input. The owning
Streaming Action may treat `DETECTED` as its near-orbit safety Gate. Capture,
permission, or malformed-image errors fail explicitly and are not converted to
`ABSENT`.
