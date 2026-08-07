# Elite Dangerous request docking range classifier

This pure internal Action accepts one current raw OCR result, extracts exactly
one numeric distance with a recognized `m`, `km`, `Mm`, `ls`, or `Ly` suffix,
normalizes it to metres, and compares the displayed distance with the strict
`distanceMeters < 7500` request-docking Gate.

OCR confidence below `0.55`, missing or malformed numeric text, an unrecognized
unit, or more than one distance candidate returns `UNKNOWN`. The classifier
does not repair OCR, reuse earlier distance, infer the current target, or use
Journal and status files. A displayed `7.50km` is `DENIED`.
