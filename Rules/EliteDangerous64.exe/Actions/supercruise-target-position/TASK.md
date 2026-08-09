# Elite Dangerous Supercruise target position

This finite composite Action uses the central PP-OCR text-region capture to
find exactly one current-frame label matching `targetName`. In the reviewed
Supercruise HUD the marker is immediately left of that label, so the Action
applies the declared 30 by 8 reference-pixel label-to-marker offset and reports
the marker displacement from the 1920x1080 screen centre. Missing, duplicate,
or low-confidence labels return `UNKNOWN`; Compass evidence is not reused.
