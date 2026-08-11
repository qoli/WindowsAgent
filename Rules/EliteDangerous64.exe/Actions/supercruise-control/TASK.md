# Elite Dangerous Supercruise control

This finite Action resolves the dedicated Frontier `Supercruise` control from
the currently active bindings preset and injects it once for 80 ms. Live
normal-space testing showed that the former 40 ms F9 injection could complete
at the input layer without producing any charging prompt, while a later 40 ms
diagnostic did work. The 80 ms press spans a wider game input-polling window
without introducing a second toggle or another binding source. It does not use
`HyperSuperCombination` as a fallback, because a combined FSD command can
select hyperspace when a route target is active. Successful output proves only
that the configured key was injected; callers must visually verify charging,
entry, safe-disengage readiness, and exit.
