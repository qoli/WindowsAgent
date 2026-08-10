# Elite Dangerous hyperspace jump to one System

This interruptible linear Streaming Action owns exactly one hyperspace
jump to an exact System. With `targetLockConfirmed=false` it verifies or
acquires the named Navigation target. With `targetLockConfirmed=true` it uses
the caller's explicit evidence boundary and does not reopen Navigation. It then
coarsely aligns at 0% through the Compass, requires a current stellar
obstruction Gate, and invokes visible-target fine alignment only while the
forward view is `CLEAR`. It rechecks the obstruction after fine alignment,
records the latest allowlisted navigation Journal timestamp, invokes only the binding-resolved
`HyperSuperCombination`, and requires FSD charging. A newer `FSDJump` matching
both the target name and optional SystemAddress is the primary arrival Gate.
Two stable cockpit-HUD-absent samples remain a visual transition path when the
Journal evidence is unavailable.

If neither visual charging nor a newer matching Journal `StartJump` appears
within approximately five seconds, the owning Action reissues the same resolved
FSD control exactly once. A Journal `StartJump` suppresses that retry and can
also trigger the required 100% throttle, preventing a blind second keypress
from cancelling an already accepted charge.

`PARTIAL` or `BLOCKING` stellar evidence before FSD delegates to
`clear-hyperspace-occlusion`. That child turns away through measured coverage
trends, enters or reuses dedicated Supercruise, and finishes at 0% after a
bounded tangential escape. The parent then realigns and repeats both the
pre-fine and post-fine obstruction Gates. At most two such escapes are allowed;
FSD input is never sent through a currently obstructed target line.

On a matching `FSDJump`, or on the first returning cockpit-HUD sample in the
visual path, the Action immediately commands 0% throttle. Two persistent
Supercruise HUD samples are then required before completion. This arrival brake
is deliberately owned by the fast workflow rather than a higher-model callback.

`NORMAL_SPACE` and `SUPERCRUISE` are explicit start modes with matching caller
confirmations. A Supercruise start uses the Supercruise Compass control profile.
An alignment-required prompt after charging commands 0%, runs the same
supervised alignment children, and restores 100% only after they complete.

The Action does not read `NavRoute.json`, choose a later route hop, select a
Station, approach a Station, or dock. Missing target rows, ambiguous visual
evidence, missing bindings, unconfirmed charging, arrival transition failure,
child failure, cancellation, or missing Supercruise arrival evidence fails
explicitly and invokes registered 0% compensation. It does not issue more than
one evidence-gated retry or substitute another control source.
