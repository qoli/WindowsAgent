# Elite Dangerous Supercruise control

This finite Action resolves the dedicated Frontier `Supercruise` control from
the currently active bindings preset and injects it once. It does not use
`HyperSuperCombination` as a fallback, because a combined FSD command can
select hyperspace when a route target is active. Successful output proves only
that the configured key was injected; callers must visually verify charging,
entry, safe-disengage readiness, and exit.
