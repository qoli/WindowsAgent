# Elite Dangerous hyperspace jump to one System

This interruptible linear Streaming Action owns exactly one hyperspace
jump to an exact System. With `targetLockConfirmed=false` it verifies or
acquires the named Navigation target. With `targetLockConfirmed=true` it uses
the caller's explicit evidence boundary and does not reopen Navigation. It first
requires the current stellar `safeToCharge` Gate and completes any necessary
stellar-angle clearance. Only then does it align at 0% through the strict
`HYPERSPACE_CHARGE` Compass purpose (ten-pixel entry, then three consecutive
SOLID observations within the twelve-pixel verification band). This is only a
bounded handoff to the required `align-visible-target` child, which owns exact
reticle centering before any FSD input. A current `DASHED` three-quarter
destination reticle is acceptable under exact identity and stable-centre
evidence; hyperspace does not require the presentation to become `SOLID`. It
passes `centerHintConfirmed=true` only after the exact route target and Compass
handoff are established; the visible child still requires a fresh local
reticle detection before it can steer. It
immediately rechecks substantial stellar coverage
from that Compass-aligned target line before visible-target fine alignment, so
a destination behind the arrival star is cleared before its reticle becomes
washed out. It then fine-aligns, rechecks substantial stellar coverage again,
and requires three fresh numeric heat
observations at or below 60%. Only after all of those Gates pass may it send FSD
input. The ordinary stellar Gate deliberately runs before target alignment. A
post-alignment recheck ignores only small orange-ratio contamination from the
centered destination reticle; a `BLOCKING` result, at least 0.08 total stellar
coverage, or at least 0.10 central coverage forces another bounded stellar
escape and complete realignment. The Action then records the latest allowlisted navigation Journal timestamp and invokes only the binding-resolved
`HyperSuperCombination`, and requires FSD charging. A newer `FSDJump` matching
the target name case-insensitively but otherwise exactly, plus the optional
SystemAddress, is the primary arrival Gate. This accepts an all-caps HUD name
against the Journal's canonical title casing without fuzzy identity matching.
Two stable cockpit-HUD-absent samples remain a visual transition path when the
Journal evidence is unavailable.

During the FSD countdown and cockpit transition, `hyperspace-state` may lose a
WGC region read while the game replaces its render surface. The owning workflow
skips at most five explicitly identified persistent-WGC region capture errors
across that transition and emits each skipped sample. A skipped frame does not
count as charging, cockpit absence, or cockpit return. A sixth such error, or
any non-WGC child failure, remains fatal and retains the registered 0% throttle
compensation. This bounded retry never substitutes an old frame or another
state source.

If neither visual charging nor a newer matching Journal `StartJump` appears
within approximately five seconds, the owning Action reissues the same resolved
FSD control exactly once. A Journal `StartJump` suppresses that retry and can
also trigger the required 100% throttle, preventing a blind second keypress
from cancelling an already accepted charge.

`PARTIAL` or `BLOCKING` stellar evidence before FSD delegates to
`clear-hyperspace-occlusion` with the jump Action's explicit start mode before
either alignment child is allowed to run. From
normal space that child enters dedicated Supercruise and completes a bounded
tangential escape. From an existing Supercruise arrival it never toggles FSD;
it turns at 0% until the stellar view is stably `CLEAR`. The parent then runs the
strict Compass and visible-target Gates. At most two such escapes are allowed;
FSD input is never sent through a currently obstructed target line.

Compass and stellar evidence never substitute for visible-target completion.
An UNKNOWN or ambiguous visible target, deadline, WGC, schema, runtime, or any other child
failure is terminal before FSD control. If the game nevertheless returns
`ALIGNMENT_REQUIRED` after charge begins, the Action cancels that charge and
fails explicitly; it does not perform attitude correction under active charge.

On a matching `FSDJump`, or on the first returning cockpit-HUD sample in the
visual path, the Action immediately commands 0% throttle. Two persistent
Supercruise HUD samples are then required before completion. This arrival brake
is deliberately owned by the fast workflow rather than a higher-model callback.

`NORMAL_SPACE` and `SUPERCRUISE` are explicit start modes with matching caller
confirmations. A Supercruise start uses the Supercruise Compass control profile.
An alignment-required prompt after charging commands 0%, cancels the active
charge, and fails explicitly. It never tries to steer while charging.

The Action does not read `NavRoute.json`, choose a later route hop, select a
Station, approach a Station, or dock. Missing target rows, ambiguous visual
evidence, missing bindings, unconfirmed charging, arrival transition failure,
child failure, cancellation, or missing Supercruise arrival evidence fails
explicitly and invokes registered 0% compensation. It does not issue more than
one evidence-gated retry or substitute another control source.
