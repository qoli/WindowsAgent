# Select and lock a Navigation destination

This interruptible linear Streaming Action accepts an exact visible Navigation
row name and owns the complete interaction from the forward cockpit view. It
opens the left panel when absent, reads the active four-state tab directly,
cycles with `NEXT_PANEL` until NAVIGATION is observed, locates
the named row through current PP-OCR boxes, moves focus one settled row at a
time, opens its detail card, activates the confirmed focused `LOCK DESTINATION`
tile, and requires two bracket-bearing observations of the exact named row.
Either bracket is sufficient because PP-OCR may crop one edge of a skewed HUD
text box; each observation reports `BOTH`, `LEADING_ONLY`, or `TRAILING_ONLY`
bracket evidence. A normal focused row has neither bracket and is not accepted.

The compact forward-view HUD can place unrelated orange pixels under more than
one calibrated tab sample and therefore honestly return `UNKNOWN` instead of
`ABSENT`. When Navigation text is also absent, the Action performs at most two
settled `FOCUS_LEFT_PANEL` probes and requires a known four-tab state before it
continues. This is a bounded state probe, not permission to reinterpret
`UNKNOWN` as an absent panel. The probe is reported in stream events and the
Action closes the panel it established on failure.

Within NAVIGATION, an already bracket-bearing named row is direct destination
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
A unique normalized exact-name match remains selectable even when a similarly
named neighboring system lowers the fuzzy runner-up margin; duplicate exact
names remain ambiguous and are rejected.
Exact matches use a linear scan and do not spend edit-distance steps on every
other visible row. Fuzzy similarity is evaluated only when no exact row exists,
so the full eight-input visible-list traversal remains inside the declared
Starlark step budget.
When the requested name contains digits, a fuzzy row must preserve the exact
digit sequence. A different numeric identity is rejected rather than treated
as a tolerable OCR edit, so `23 ARIETIS` cannot authorize movement for
`47 ARIETIS`.
Keyboard focus is the unique strongest row-fill sample, not merely every row
above one absolute threshold. The strongest sample must reach 0.40 and lead
the runner-up by 0.10, which tolerates HUD skew while rejecting ambiguous focus.
When the exact requested row is itself the strongest sample and reaches 0.60,
that stronger direct evidence is sufficient even if OCR context padding makes
an adjacent row's runner-up ratio too close for the weaker 0.40 path. Events
report the leader, runner-up, and margin so context spill remains observable.
OCR regions consisting only of one or more `X` glyphs are excluded from focus
leadership because the Navigation filter-clear icon can be recognized as `X`
or `XX` and otherwise outscore the selected row. Legitimate destination names
that merely contain `X` remain eligible.
