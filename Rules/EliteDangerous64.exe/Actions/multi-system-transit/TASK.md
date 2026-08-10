# Elite Dangerous multi-System transit

This interruptible linear Streaming Action follows one frozen game-plotted
`NavRoute.json` plan and stops at 0% throttle in Supercruise in the final
System. It does not select or approach a Station and does not dock.

At startup it calls `elite-dangerous/filesystem/nav-route` and passes the RAW
result to `elite-dangerous/nav-route-plan`. It then matches the live
`Status.json Destination` name and SystemAddress to exactly one frozen hop. A
first-hop match starts normally; a later-hop match resumes after the preceding
hops and emits `ROUTE_RESUMED`. A destination outside the frozen route fails.
A docked start delegates departure
to `leave-station`; its supervised Auto Launch boundary remains visible. Every
route hop delegates exactly one jump to `hyperspace-jump-to-system`. The first
hop uses the caller's explicit start mode; subsequent hops start from confirmed
Supercruise. The child owns target-lock verification, alignment, FSD control,
bounded Journal `FSDJump` evidence, arrival braking, and Supercruise
confirmation.

Before each hop, the workflow requires one AVAILABLE `Status.json` snapshot
with numeric `Fuel.FuelMain`. Fuel must be at or above the explicit
`minimumFuelMain`. ED's Status snapshot does not provide a temperature field,
so this Action does not claim a temperature Gate. After each hop it requires a
newer source timestamp, then records fuel and freshness evidence. The
caller must separately assert `routeFuelConfirmed=true` because the Status
snapshot does not expose the fuel cost of every future route hop.

After every hop, including the final hop, the workflow re-reads NavRoute and
requires the same route identity. A missing, cleared, rewritten, malformed, or
destination-mismatched route fails explicitly. So do unavailable Status,
insufficient fuel, a missing post-hop timestamp transition, target mismatch,
child failure, or cancellation. All failure paths retain registered 0% throttle compensation.
No network route, prior route, alternate binding, or hidden perception fallback
is used. Journal access is restricted to the allowlisted bounded navigation
tail Action.
