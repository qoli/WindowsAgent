# Select and lock a Navigation destination

This interruptible linear Streaming Action accepts an exact visible Navigation
row name and owns the complete interaction from the forward cockpit view. It
opens the left panel when absent, reads the active four-state tab directly,
cycles with `NEXT_PANEL` until NAVIGATION is observed, locates
the named row through current PP-OCR boxes, moves focus one settled row at a
time, opens its detail card, activates the confirmed focused `LOCK DESTINATION`
tile, and requires two angle-bracketed observations of the named row.

Within NAVIGATION, an already angle-bracketed named row is direct destination
lock evidence and returns `EXISTING`. This meaning is deliberately local to
NAVIGATION; angle brackets in CONTACTS describe a different ship-target lock.
For an unlocked row, highlight is only keyboard focus: the Action must move the
highlight to the named row before `SELECT` may enter its destination detail.
A confirmed focused `UNLOCK DESTINATION` detail also returns `EXISTING` without
toggling the lock. On every successful `EXISTING` or `ACQUIRED` path, the Action
closes the left panel and requires two current `ABSENT` observations before it
reports completion. `openedPanel` records whether this invocation originally
opened the panel; it does not weaken the successful forward-view postcondition.
When the Action opened the panel, it also registers the close operation as
failure compensation.

The Action scans only the currently visible Navigation list. A missing target,
ambiguous OCR match, non-unique focused row, unknown detail label, unexpected
panel state, or missing post-selection brackets fails explicitly. It never
scrolls an unbounded list, selects the nearest row, substitutes another target,
or calls the older streaming `lock-destination` Action as a hidden fallback.
