# Close Elite Dangerous Navigation detail

This internal failure-compensation Action sends one BACK to leave an open
Navigation detail card. One current `NAVIGATION` tab-header observation proves
that BACK left the detail view; the Action then delegates all remaining panel
closure and its postcondition to `close-left-panel`. Elite Dangerous may close
the panel automatically shortly after accepting a Supercruise Assist request,
so the Navigation list can remain visible for only one rendered frame. The
first fresh tab observation therefore starts 200 ms after `BACK`. The detail
card itself hides the tab pixels and may be classified as `ABSENT`, so
`ABSENT` without a preceding current-run `NAVIGATION` observation is never
proof that BACK worked. It is not a general UI fallback.
