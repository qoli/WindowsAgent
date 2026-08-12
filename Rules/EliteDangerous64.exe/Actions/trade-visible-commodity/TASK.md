# Trade one visible Commodity Market row

This interruptible linear Streaming Action trades an exact commodity that is
already visible in the currently open Elite Dangerous Commodity Market. The
caller supplies `BUY` or `SELL`, the exact commodity name, a quantity, and the
expected Station name. This first version deliberately does not open Starport
Services, change the BUY/SELL tab, or scroll the commodity list.

The Action requires two adjacent current PP-OCR cycles that prove the expected
market title, Station, mode, and exactly one matching visible commodity row.
The header and list use separate bounded captures to stay below the resident
OCR runtime's pixel limit. It clicks the exact commodity box returned by the
list capture, requires two dialog observations containing the exact
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
