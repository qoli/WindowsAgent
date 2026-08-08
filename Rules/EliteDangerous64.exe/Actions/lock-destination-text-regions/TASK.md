# Elite Dangerous Lock Destination text regions

Detect and perspective-rectify text lines in the fixed Navigation detail-card
action strip. The expected semantic labels are `LOCK DESTINATION` and `UNLOCK
DESTINATION`, but this raw OCR Action returns boxes, text, confidence, and
adjacent pixels without deciding either state.

The region is sampled at reference density because the task needs HUD text,
not native 4K detail. It does not press the highlighted button or infer that a
destination changed.
