# Elite Dangerous select Station target

This independent linear Streaming Action establishes or confirms a local
Station target lock. It is not part of `dock-at-station` and has no distance or
docking-request semantics.

The caller supplies the exact `stationName`. The Action opens and selects the
CONTACTS tab, then uses current PP-OCR text regions to find that named visible
row. Angle brackets in the recognized row, such as `< MOONGLOW CITY >`, are
the direct lock evidence. A filled row is keyboard focus only and never proves
target lock.

When the named row is visible, the Action may move focus one settled `UP` or
`DOWN` step at a time according to current row geometry. It sends `SELECT`
exactly once only after two consecutive observations prove that the named row
is focused, then requires two consecutive angle-bracketed observations. If the
row is outside the current visible list, OCR is ambiguous, or focus cannot be
proved, the Action fails explicitly instead of scanning or selecting another
contact blindly.

The Action closes the left panel on success or failure only when it opened the
panel itself. If the caller already had a panel open, that UI ownership remains
with the caller.
