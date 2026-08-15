# Clear a Supercruise Assist line-of-sight obstruction

This interruptible linear Streaming Action owns only the
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET` semantic wrapper. It does not align
the destination and does not implement a second sphere-control loop.

The Action first requires two classified
`SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` samples. `UNKNOWN` and other
expected cruise states reset this Gate; an unexpected known state fails. It
installs critical 0% failure compensation before observing the Gate.

After the Gate is stable, the wrapper invokes the shared
`elite-dangerous/fixed-supercruise-sphere-separation` Streaming Action. That
child exclusively owns current-frame sphere-direction confirmation, the fixed
6,400 ms outward turn, the fixed 30,000 ms Supercruise separation, Status
safety checks, input release, cancellation, and 0% compensation. This wrapper
accepts the child only when its complete fixed-clearance postcondition reports:

- completed fixed outward turn of exactly 6,400 ms;
- completed separation of exactly 30,000 ms;
- current Supercruise confirmation; and
- final commanded throttle of 0%.

The wrapper then obtains fresh prompt evidence. It completes only after two
positive current OCR states prove the LOS prompt is absent. `UNKNOWN` and a
still-present LOS prompt reset this Gate and never count as absence evidence.

The terminal output copies the shared child's mechanical evidence, derives
`fixedOutwardTurnCompleted=true` from the completed child plus its exact
6,400 ms fixed-turn contract, and adds the final positive prompt-clear state.
It does not claim that detector absence
proved a body edge transition. The caller must then perform the separate 4.5
handoff: Compass coarse alignment, visible focus-frame fine alignment, and only
afterward reacquire Supercruise Assist.
