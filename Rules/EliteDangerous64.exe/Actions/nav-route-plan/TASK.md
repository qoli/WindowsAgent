# Validate an Elite Dangerous multi-System NavRoute plan

This finite pure Action accepts the complete RAW result from
`elite-dangerous/filesystem/nav-route` plus the exact expected final System and
a caller-owned jump limit. It performs no file read itself.

`AVAILABLE` evidence must carry a `NavRoute` event and a Route containing at
least an origin and one destination. Every retained entry must have a trimmed
`StarSystem`, a positive integer `SystemAddress`, exactly three numeric
`StarPos` coordinates, and a non-empty `StarClass`. The final entry must match
`expectedDestinationSystem`, and the number of hops must not exceed
`maxJumps`. The Action returns a frozen ordered plan and a route identity built
from the source timestamp, final SystemAddress, and hop count.

`ABSENT`, `NavRouteClear`, a missing Route, malformed entries, duplicate
SystemAddress values, a destination mismatch, or an excessive hop count fails
explicitly. Source age is preserved as evidence but is not interpreted as
route invalidity: a plotted route is an event snapshot and can remain active
longer than the filesystem Action's current-source window. The owning workflow
must re-read and compare the route identity between hops. No network route,
prior result, guessed entry, or differently shaped event is substituted.
