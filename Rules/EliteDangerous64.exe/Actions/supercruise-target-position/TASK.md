# Elite Dangerous Supercruise target position

This finite composite Action uses two adjacent forward-HUD PP-OCR bands to
find exactly one current-frame label matching `targetName`. A candidate must
pass both OCR confidence Gates and then match the normalized requested name
exactly or by one substitution, insertion, or deletion. This bounded tolerance
handles live repeated-character loss such as `LT 11244` for `LTT 11244`; short
tokens and two-or-more-edit candidates remain rejected. In the reviewed
Supercruise HUD the marker is immediately left of that label, so the Action
applies the declared 30 by 8 reference-pixel label-to-marker offset and reports
the marker displacement from the 1920x1080 screen centre. Matches in the
band boundary are de-duplicated only when their reference boxes agree within
eight pixels; distinct duplicate labels still fail. Missing, duplicate,
or low-confidence labels return `UNKNOWN`; Compass evidence is not reused.
