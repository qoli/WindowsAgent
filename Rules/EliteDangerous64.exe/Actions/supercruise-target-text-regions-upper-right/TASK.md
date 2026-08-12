# Upper-right Supercruise target text regions

Detect current-frame target labels in reference x=1120–1920 and y=80–400.
Live Supercruise Assist evidence placed the selected `SHAW STATION` label near
the upper-right HUD edge while the centre prompt reported `ALIGN WITH TARGET
DESTINATION`; all three existing centre/lower bands therefore returned no
matching target text. The 800 by 320 region stays within the resident PP-OCR
runtime's 262144-pixel bound.

This raw Action returns boxes and text only. It neither infers target identity
nor serves as a fallback result. `supercruise-target-position` still requires a
spatially unique label matching the requested target name across every declared
band; missing, ambiguous, or low-confidence evidence remains `UNKNOWN`.
