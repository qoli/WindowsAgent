# Elite Dangerous prepare Auto Launch

This finite composite Action owns the deterministic docked-cockpit menu
sequence that services the ship and selects `AUTO LAUNCH`. Its caller must
already have established that Elite Dangerous is showing the docked cockpit
menu containing the service icon row, `STARPORT SERVICES`, `AUTO LAUNCH`, and
`DISEMBARK`; it does not exit a full-screen Starport Services page.

The Action sends four `DOWN` controls to clamp focus at `DISEMBARK`, then three
`UP` controls to enter the four-tile service row. Elite Dangerous remembers the
row's horizontal focus, so the Action obtains two consistent
`station-service-focus` observations, computes the minimum rightward cyclic
distance to Refuel, and visually confirms Refuel before `SELECT`. It then moves
right once and visually confirms Repair before the second `SELECT`. Grey service
tiles remain focusable; the grayscale classifier detects keyboard-focus fill,
not service availability.

It finally restores the `DISEMBARK` baseline and focuses `AUTO LAUNCH`.
`activateAutoLaunch=false` is the safe-test mode: it stops immediately before
the final `SELECT` and returns `awaitingFinalSelect=true`. Only
`activateAutoLaunch=true` sends that final input.

Every input is resolved through `elite-dangerous/ui-control`; the Action never
assumes physical keys. Unknown, ambiguous, inconsistent, or contradicted focus
evidence fails explicitly without a fixed-sequence fallback. A successful
activation result still proves only that the binding-resolved controls were
injected. An owning workflow must verify the subsequent `AUTO_LAUNCH` or
`WAITING_IN_QUEUE` visual state.
