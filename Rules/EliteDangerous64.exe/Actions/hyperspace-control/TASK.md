# Elite Dangerous hyperspace control

This finite Action resolves Frontier's `HyperSuperCombination` control from
the currently active binding preset and injects it once with a 120 ms press.
The longer game-facing press avoids missed 40 ms taps observed live while
remaining a bounded press rather than a held control. It is intended only
when a hyperspace destination is already locked and the ship is visually
aligned with that destination.

Successful output proves only that the configured key was injected. The
caller must independently observe FSD charging and the subsequent hyperspace
arrival. It does not select a destination, align the ship, change throttle, or
fall back to the dedicated `Supercruise` binding.
