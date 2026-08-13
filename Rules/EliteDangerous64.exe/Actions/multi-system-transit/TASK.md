# Elite Dangerous multi-System transit

This interruptible linear Streaming Action follows one frozen game-plotted
`NavRoute.json` plan and stops at 0% throttle in Supercruise in the final
System. It does not select or approach a Station and does not dock.

At startup it calls `elite-dangerous/filesystem/nav-route` and passes the RAW
result to `elite-dangerous/nav-route-plan`. It then matches the live
`Status.json Destination` name and SystemAddress to exactly one frozen hop. A
first-hop match starts normally; a later-hop match resumes after the preceding
hops and emits `ROUTE_RESUMED`. A destination outside the frozen route fails.
If a game reconnect preserves the exact NavRoute but removes
`Status.json Destination`, the Action may recover only from the newest
allowlisted Journal route-position event. A login `Location` or a completed
`FSDJump` must place the ship exactly at the route origin or a non-final frozen
hop. It then emits
`RESTORING_ROUTE_TARGET`, sends the binding-resolved Frontier
`TargetNextRouteSystem` control once, and waits for a newer Status snapshot to
report the same unique next-hop name and SystemAddress before any flight
command. If five bounded Status observations remain without Destination, the
Action delegates `plot-route-to-system` in its exact existing-route context
refresh mode, requires the same route identity, sends `TargetNextRouteSystem`
once more, and then returns to the ordinary exact Status readiness Gate. This
handles the game reconnect state where `NavRoute.json` and the Galaxy Map route
remain valid but the cockpit target context is not initialized. Missing,
out-of-route, or already-final Journal position evidence
fails explicitly; an
existing mismatched Status destination is never overwritten by this recovery.
A docked start delegates departure
to `leave-station`; its supervised Auto Launch boundary remains visible. Every
route hop delegates exactly one jump to `hyperspace-jump-to-system`. The first
hop uses the caller's explicit start mode; subsequent hops start from confirmed
Supercruise. The child owns target-lock verification, alignment, FSD control,
bounded Journal `FSDJump` evidence, arrival braking, and Supercruise
confirmation.

After every non-final `FSDJump`, the workflow unconditionally delegates an
arrival-star clearance before it begins the next hop. The child uses the
existing-Supercruise CV mode: turn at 0% until the stricter `safeToCharge`
Gate is stable, add two seconds of angular margin, fly the safe heading at 100%
for 24 seconds, then return to 0% and cool to the child's 45% handoff Gate. This lowers
the next alignment and charge failure probability by moving the ship away from
the arrival star before the next hyperspace target is pursued. Its final
`CLEAR`, Supercruise, and 0%-throttle postconditions are all required.

Before each hop, the workflow requires one AVAILABLE `Status.json` snapshot
with numeric `Fuel.FuelMain`. Fuel must be at or above the explicit
`minimumFuelMain`. ED's Status snapshot does not provide a temperature field,
so this Action does not claim a temperature Gate. After each hop it requires a
newer source timestamp, then records fuel and freshness evidence. The
caller must separately assert `routeFuelConfirmed=true` because the Status
snapshot does not expose the fuel cost of every future route hop.

After every intermediate hop, the workflow re-reads NavRoute and requires the
same route identity. A missing, cleared, rewritten, malformed, or
destination-mismatched intermediate route fails explicitly. After the final
child jump and fresh Status readiness prove the final frozen hop, the workflow
does not re-read NavRoute: Elite Dangerous normally writes `NavRouteClear` on
successful route completion, so that file is no longer a valid terminal Gate.
Unavailable Status,
insufficient fuel, a missing post-hop timestamp transition, target mismatch,
child failure, or cancellation. All failure paths retain registered 0% throttle compensation.
No network route, prior route, alternate binding, or hidden perception fallback
is used. Journal access is restricted to the allowlisted bounded navigation
tail Action.
