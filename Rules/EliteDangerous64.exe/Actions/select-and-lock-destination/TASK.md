# Select and lock a Navigation destination

This interruptible linear Streaming Action accepts an exact visible Navigation
row name and owns the complete interaction from the forward cockpit view. It
opens the left panel when absent, establishes the CONTACTS tab as an observed
cycle anchor, moves exactly two `PREVIOUS_PANEL` steps to NAVIGATION, locates
the named row through current PP-OCR boxes, moves focus one settled row at a
time, opens its detail card, activates the confirmed focused `LOCK DESTINATION`
tile, and requires two angle-bracketed observations of the named row.

An already angle-bracketed row or a confirmed focused `UNLOCK DESTINATION`
detail returns `EXISTING` without toggling the lock. When the Action opened the
panel, it restores the forward view on success and registers the same close
operation as failure compensation.

The Action scans only the currently visible Navigation list. A missing target,
ambiguous OCR match, non-unique focused row, unknown detail label, unexpected
panel state, or missing post-selection brackets fails explicitly. It never
scrolls an unbounded list, selects the nearest row, substitutes another target,
or calls the older streaming `lock-destination` Action as a hidden fallback.
