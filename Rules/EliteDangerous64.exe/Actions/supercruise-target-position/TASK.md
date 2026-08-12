# Elite Dangerous Supercruise target position

This finite composite Action uses two adjacent forward-HUD PP-OCR bands to
find exactly one current-frame label matching `targetName`. A candidate must
pass both OCR confidence Gates and then match the normalized requested name
exactly or by one substitution, insertion, or deletion. This bounded tolerance
handles live repeated-character loss such as `LT 11244` for `LTT 11244`; short
tokens and two-or-more-edit candidates remain rejected. In the reviewed
Supercruise HUD the marker is immediately left of that label, so the Action
applies the declared 30 by 8 reference-pixel label-to-marker offset and reports
the marker displacement from the 1920x1080 screen centre. Matches at a band
boundary are de-duplicated when their reference boxes agree within 16 pixels;
live overlapping lower bands placed the same label 8.76 pixels apart. Elite
also repeats the selected destination in the lower-left
information panel. When distinct same-name candidates remain, this Action
selects the label whose derived marker is nearest the forward-screen centre
only if it leads the next candidate by at least 32 reference pixels. Closer
candidates remain explicitly ambiguous. Missing, ambiguous, or low-confidence
labels return `UNKNOWN`; Compass evidence is not reused.
