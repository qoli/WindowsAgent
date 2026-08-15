# Set a deterministic Commodity Market view

This internal interruptible linear Streaming Action mechanically normalizes the
currently open Elite Dangerous Commodity Market into one of two caller-owned
views. It does not use OCR, infer retained filter state, or choose a commodity.
The caller must already have proved that the exact Station market is open.

`BUY_ALL_GOODS` replays this exact control structure from the market's reset
entry focus:

1. `DOWN` three times and `SELECT` to open Filters;
2. `DOWN` twenty times and `RIGHT` five times to clamp focus to Reset Filters;
3. `SELECT` to reset, `LEFT` five times, `RIGHT` once, and `SELECT` to Apply;
4. `UP` three times and `SELECT` to activate BUY; and
5. `RIGHT` once to focus the first goods row.

`SELL_SINGLE_CARGO` replays this exact control structure:

1. `DOWN` three times and `SELECT` to open Filters;
2. `DOWN` twenty times, `RIGHT` twice, and `SELECT` to reset filters;
3. `UP` ten times, `RIGHT` once, and `SELECT` to select Cargo;
4. `DOWN` fifteen times, `LEFT` three times, `RIGHT` once, and `SELECT` to Apply;
5. `UP` twice and `SELECT` to activate SELL; and
6. `RIGHT` once to focus the only sellable cargo row.

The directional counts intentionally clamp retained filter focus at UI edges.
They are fixed primary behavior, not retries or fallback. Any child input
failure is terminal and stops the remaining sequence. Completion proves only
that every binding-resolved input returned successfully. It reports
`filterReplayCompleted=true` and `listFocusCommanded=true`; it does not claim
that filter state or a commodity identity was visually observed. BUY callers
must still use exact OCR before choosing a commodity. SELL callers must prove
through Cargo.json before this Action that exactly one commodity exists and
must prove an exact newer Cargo delta after submission.
