# Elite Dangerous Supercruise target position

This finite composite Action uses two overlapping wide forward-HUD PP-OCR bands to
find exactly one current-frame label matching `targetName`. In the reviewed
Supercruise HUD the marker is immediately left of that label, so the Action
applies the declared 30 by 8 reference-pixel label-to-marker offset and reports
the marker displacement from the 1920x1080 screen centre. Matches in the
40-reference-pixel band overlap are de-duplicated only when their reference
boxes agree within eight pixels; distinct duplicate labels still fail. Missing, duplicate,
or low-confidence labels return `UNKNOWN`; Compass evidence is not reused.
