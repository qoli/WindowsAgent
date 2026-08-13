# Trade one exact Commodity Market row

This interruptible linear Streaming Action trades an exact commodity in the
currently open Elite Dangerous Commodity Market. The caller supplies `BUY` or
`SELL`, the exact commodity name, a quantity, and the expected Station name.
It does not open Starport Services or change the BUY/SELL mode. When the exact
commodity is not initially visible, it resets focus through the current mode
tile, enters the first commodity row, and uses at most 180 binding-resolved
`UI_Down` steps in ten-row batches. Every batch is followed by fresh OCR;
there is no unbounded navigation or fuzzy name match.

The Action requires two adjacent current PP-OCR cycles that prove the expected
market title, Station, mode, and exactly one matching visible commodity row.
The header, upper list/dialog slice, and lower GOODS-column slice use separate
bounded captures to stay below the resident OCR runtime's pixel and detector
shape limits. The two non-overlapping list slices cover every row currently
visible in the market rather than only its upper portion. It clicks the exact
commodity box returned by those list captures, immediately activates that
focus with binding-resolved `UI_Select`,
requires two dialog observations containing the exact
commodity and matching `BUY COMMODITY` or `SELL COMMODITY` title, changes the
quantity with binding-resolved `RIGHT`, focuses the matching confirmation
button with `DOWN`, and sends `SELECT` once.

Input success is not trade success. Before input, the Action reads an available
Cargo.json snapshot and records the named inventory count, source timestamp,
and reported freshness. The initial snapshot may be `UNKNOWN` freshness:
Cargo.json is an event snapshot and unchanged inventory does not become false
after fifteen seconds. It completes only after a snapshot with a different
source timestamp contains the exact expected count delta. It then
sends the shared `exit-commodity-market` cleanup Action, which spaces two
`BACK` inputs across the actual UI transitions, and requires two header
observations where `COMMODITIES MARKET` is absent. This proves exit from the
market, not the exact resulting docked cockpit screen; the supervising model
must use a fresh capture when that goal-layer distinction matters. Missing,
stale, ambiguous, or
contradictory OCR and Cargo evidence fails explicitly. Unknown initial
freshness never weakens the required newer timestamp and exact post-trade
delta. It never substitutes a
different commodity, previous inventory, Market.json, Journal lines, or a
pointer-only success claim. Quantity focus and submit inputs are separated by
bounded UI-settle intervals rather than emitted back-to-back. Quantity changes
are spaced by 60 ms; durable progress events are emitted for the first step,
every twenty-five steps, and the requested final step rather than once per
tonne.

If the exact item cannot be found before the bounded navigation limit, or if
market identity/mode changes during search, the Action fails without guessing
another row. OCR-derived geometry remains click authority only for the same
exact commodity box; keyboard navigation is only a search mechanism.
