# Upper-left Supercruise target text regions

Detect current-frame target labels in reference x=0–800 and y=80–400. Live
post-jump evidence placed `LP 298-42` at reference x=258, y=390 after Compass
alignment while every existing central, lower, and upper-right band returned no
matching text. The 800 by 320 region stays within the resident PP-OCR runtime's
262144-pixel bound.

This raw Action returns boxes and text only. It neither infers target identity
nor serves as a fallback result. `supercruise-target-position` still requires a
spatially unique label matching the requested target name across every declared
band; missing, ambiguous, or low-confidence evidence remains `UNKNOWN`.
