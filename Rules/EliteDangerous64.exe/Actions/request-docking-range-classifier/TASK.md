# Elite Dangerous request docking range classifier

This pure internal Action accepts one current PP-OCR text-regions result,
extracts exactly one numeric distance from one recognized region with an `m`,
`km`, `Mm`, `ls`, or `Ly` suffix,
normalizes it to metres, and compares the displayed distance with the strict
`distanceMeters < 7500` request-docking Gate.

Detection confidence below `0.70`, recognition confidence below `0.75`, missing
or malformed numeric text, an unrecognized unit, or more than one distance
candidate returns `UNKNOWN`. The classifier does not concatenate regions,
repair OCR such as `kkm`, reuse earlier distance, infer the current target, or
use Journal and status files. A displayed `7.50km` is `DENIED`.
