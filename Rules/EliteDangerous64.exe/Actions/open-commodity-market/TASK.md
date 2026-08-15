# Open and normalize Commodity Market

This interruptible linear Streaming Action owns the transition from the
current docked-cockpit menu to a deterministic Commodity Market view for the
caller's exact `BUY` or `SELL` operation. It requires two current resident
PP-OCR observations containing all three docked menu labels before input.
Repeated `DOWN` clamps focus at `DISEMBARK`; two `UP` inputs then focus
`STARPORT SERVICES`, which is selected once. After the services transition,
the Action clicks the fixed Rule-owned Commodity Market tile to establish
focus, then sends binding-resolved `SELECT` to activate it. Elite Dangerous
does not reliably accept an injected pointer click as activation on this
surface even though the tile becomes visibly highlighted.

After two current header observations identify `COMMODITIES MARKET`, the exact
Station, and the current mode, the Action calls the internal
`set-commodity-market-view` module. BUY always executes `BUY_ALL_GOODS`: it
mechanically resets persistent filters, applies the empty filter set, activates
BUY, and focuses the first goods row. SELL always executes
`SELL_SINGLE_CARGO`: it resets filters, selects Cargo only, applies, activates
SELL, and focuses the first sellable cargo row. Fixed directional counts clamp
retained filter focus at UI edges. This is the primary contract, not a retry or
fallback.

The Action never treats navigation input alone as the market-mode
postcondition. After the fixed replay it requires two fresh header observations
containing `COMMODITIES MARKET`, the exact Station, and the requested
`BUY FROM MARKET` or `SELL TO MARKET`. It reports the replayed profile and
control count honestly, but does not claim filter contents were visually
observed. The successful postcondition leaves the prepared list open for
`trade-visible-commodity`.

Failure cleanup is registered only after the initial docked cockpit menu has
been confirmed twice, but before entering Starport Services. From that proven
starting state, `exit-commodity-market` safely restores the cockpit if market
opening, the fixed view replay, or final header validation fails. No BACK is
sent when the initial docked menu is ambiguous. OCR ambiguity, an unexpected
Station, an invalid child output, or a final mode mismatch fails explicitly.
No other service, mode, filter profile, coordinate, or algorithm is
substituted.

Failure cleanup allows one extra conditional BACK because a failed mechanical
view replay may leave the Filters overlay open. This is cleanup only; it does
not substitute an alternate market path.
