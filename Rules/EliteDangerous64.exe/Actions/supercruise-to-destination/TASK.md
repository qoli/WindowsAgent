# Supercruise to a locked destination

This interruptible linear Streaming Action owns the visual flight sequence
from a confirmed Navigation destination lock to a safe normal-space arrival.
The first target is `NAV BEACON`, but the workflow does not encode that name.

The caller must first complete `elite-dangerous/select-and-lock-destination`
and pass its confirmed target name with `targetLocked=true`, plus
`normalSpaceConfirmed=true` from the preceding visual flight state. Streaming Actions
cannot synchronously call another Streaming Action, so this precondition is an
explicit lifecycle boundary rather than hidden high-level UI work.

The workflow requires current visual `MASS LOCK`, `LANDING GEAR`, and
`CARGO SCOOP` states all to be `OFF`; stops and aligns against the Compass;
invokes the dedicated Frontier `Supercruise` binding; and requires an observed
FSD charging state followed by visual `SUPERCRUISE` confirmation. It then selects the bound 75% throttle control and
keeps the solid Compass marker within a 16-reference-pixel approach zone.

Disengage occurs only after two consecutive OCR classifications of
`SAFE DISENGAGE READY`. After the dedicated Supercruise toggle, the workflow
commands 0% and requires three consecutive `ship-speed` `STOPPED` observations
backed by the slashed-zero topology. Prompt disappearance or elapsed time is
never accepted as arrival. Once FSD movement can begin, failure compensation
commands 0% throttle on every terminal failure or cancellation.

No binding fallback is used: missing `Supercruise` or `SetSpeed75` bindings
fail with their original binding-resolution errors. In particular,
`HyperSuperCombination` is not substituted because it can initiate hyperspace
when another route target is active.
