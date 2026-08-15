# Buy one exact commodity or mechanically sell one cargo commodity

This interruptible linear Streaming Action trades in the deterministic market
view produced by `open-commodity-market`. The caller supplies `BUY` or `SELL`,
the exact commodity name, a quantity, and the expected Station. It does not
open Starport Services or change filters itself.

BUY consumes `BUY_ALL_GOODS` and retains the exact OCR contract. When the
commodity is not initially visible, it enters the goods list and uses at most
180 binding-resolved `DOWN` steps in ten-row batches. Every batch is followed
by fresh OCR; there is no unbounded navigation or fuzzy match. Two adjacent
current PP-OCR cycles must prove the expected market title, Station, BUY mode,
and exactly one matching visible row. Separate header and list slices stay
below resident OCR shape limits. The same-frame exact commodity box is click
authority. The Action activates it with binding-resolved `SELECT` and requires
two dialog observations containing the exact commodity and `BUY COMMODITY`.

SELL consumes `SELL_SINGLE_CARGO` without OCR. Before any market input it
requires Cargo.json to contain exactly one positive, non-stolen inventory
entry; its exact name must equal `commodityName`, and its count must equal the
requested quantity. Because the prepared view contains only that one sellable
cargo row and leaves it focused, SELL sends `SELECT`, advances the exact full
quantity, and submits mechanically. Multiple cargo commodities, partial
quantity requests, a mismatched name, or stolen cargo fail before input. There
is no OCR list search, dialog OCR, alternate row selection, or fallback in the
SELL path.

Input success is not trade success. Before input, the Action records the
Cargo.json count, source timestamp, and freshness. Initial freshness may be
`UNKNOWN`: Cargo.json is an event snapshot. Both paths complete only after a
snapshot with a different source timestamp contains the exact expected count
delta. Quantity changes are spaced by 60ms, while progress events cover the
first step, every twenty-five steps, and the requested final step.

After the exact delta, the shared `exit-commodity-market` Action sends two
spaced `BACK` inputs. BUY additionally requires two header observations where
`COMMODITIES MARKET` is absent. Mechanical SELL deliberately does not add OCR
after the sale; its output claims cleanup command completion, not visual market
absence. The supervising model must use a fresh capture for the resulting
cockpit goal. Missing, stale, ambiguous, or contradictory required evidence is
terminal. No previous inventory, Market.json, Journal line, alternate
commodity, pointer-only claim, or hidden fallback is accepted.
