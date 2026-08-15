# Clear a Supercruise Assist line-of-sight obstruction

This interruptible linear Streaming Action owns the fast response to
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET`. It does not align the destination.

After two classified `SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` samples, it
commands 0% and installs critical 0% failure compensation. The internal
`supercruise-sphere-direction` observation module must then produce two fresh,
compatible `DETECTED` observations whose direction is `READY` and whose
selected control is identical. These observations select one outward direction
and are the only sphere evidence with control authority.

The Action then executes exactly eight bounded 800 ms attitude pulses in that
fixed direction, for a total commanded turn duration of 6,400 ms. Each
diagonal pulse owns a separate leased START/STOP pair and registers key-release
failure compensation while the lease is active. The direction is never
recomputed, reversed, or retried. In particular, later sphere `ABSENT`,
`UNKNOWN`, dashboard occlusion, or prompt changes cannot shorten the fixed turn;
the implementation deliberately performs no sphere observations after direction
confirmation.

After the fixed turn, attitude input stops. The Action commands 100% for exactly
30 seconds, polling current flight status every 500 ms, then unconditionally
commands 0%. It completes only after two positive current OCR states prove the
LOS prompt is absent. `UNKNOWN` resets this Gate and never counts as absence
evidence.

The terminal postcondition is a completed 6,400 ms fixed outward turn, a
completed 30,000 ms separation lease, positive prompt-clear evidence, and
commanded 0% throttle. It does not claim that detector absence proved a body
edge transition. The caller must then perform the separate 4.5 handoff: Compass
coarse alignment, visible focus-frame fine alignment, and only afterward
reacquire Supercruise Assist.
