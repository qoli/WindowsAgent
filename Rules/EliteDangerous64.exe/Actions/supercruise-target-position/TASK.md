# Elite Dangerous Supercruise target position

This finite composite Action uses one tall central and two lower forward-HUD
PP-OCR bands to find exactly one current-frame label matching `targetName`.
The central band covers reference `y=80..400`: live Supercruise evidence placed
an already Compass-aligned planetary marker around `y=185`, above the retired
`y=240..400` strip. Its 800 by 320 shape retains the same 256k reference-pixel
budget. A candidate must
pass both OCR confidence Gates and then match the normalized requested name
exactly or by one substitution, insertion, or deletion. This bounded tolerance
handles live repeated-character loss such as `LT 11244` for `LTT 11244`; short
tokens and two-or-more-edit candidates remain rejected. In the reviewed
Supercruise HUD the marker is immediately left of and below that label. A live
4K frame placed the OCR label left edge at reference `x=988.73` and its centre
at `y=537.5`; the orange destination ring measured `x=960`, `y=550`. The
Action therefore derives the marker 30 reference pixels left of the label and
12.5 pixels below its centre, then reports its displacement from the 1920x1080
screen centre. Matches at a band
boundary are de-duplicated when their reference boxes agree within 16 pixels;
live overlapping lower bands placed the same label 8.76 pixels apart. Elite
also repeats the selected destination in the lower-left
information panel. When distinct same-name candidates remain, this Action
selects the label whose derived marker is nearest the forward-screen centre
only if it leads the next candidate by at least 32 reference pixels. Closer
candidates remain explicitly ambiguous. Missing, ambiguous, or low-confidence
labels return `UNKNOWN`; Compass evidence is not reused.

Some Station labels near the lower cockpit edge are split into two lines and
partly occluded by the dashboard. For an exact two-word `targetName`, the
Action also accepts one same-frame two-line candidate only when both OCR boxes
pass the normal confidence Gates, their left edges differ by at most 16
reference pixels, their vertical centres are 12 to 36 pixels apart in
top-to-bottom order, and each normalized line contains at least four characters
matching the corresponding requested word prefix with at most one edit. This
recognizes reviewed `CREL` / `STAN` evidence for `CREON'S STANDING` without
accepting arbitrary partial text or dropping target identity. The combined
two-line box, not either fragment alone, owns marker geometry. Other word
counts and unmatched fragments remain `UNKNOWN`.
