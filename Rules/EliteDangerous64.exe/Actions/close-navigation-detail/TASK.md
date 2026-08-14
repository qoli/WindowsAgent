# Close Elite Dangerous Navigation detail

This internal failure-compensation Action sends one BACK to leave an open
Navigation detail card. One current, single-region Navigation-tab highlight
observation proves that BACK left the detail view; the Action then delegates
all remaining panel closure and its postcondition to `close-left-panel`. The
specialized observation reads one 4x4 sample instead of the ordinary four
sequential tab samples. Elite Dangerous may close
the panel automatically shortly after accepting a Supercruise Assist request,
so the Navigation list can remain visible for only one rendered frame. The
first fresh tab observation therefore starts 200 ms after `BACK`. The detail
card itself hides the tab pixels and may be classified as `ABSENT`, so
`ABSENT` without a preceding current-run `NAVIGATION` observation is never
proof that BACK worked. It is not a general UI fallback.

When the caller supplies `detailLabelConfirmed=true`, it is also asserting that
the contextual `SUPERCRUISE ASSIST` label was positively read immediately
before this Action. If the one-frame Navigation list is missed, the Action may
accept the game's automatic close only after two fresh observations both show
the complete four-tab header as `ABSENT` and the same detail-card OCR region no
longer contains a qualified Supercruise Assist label. Either signal alone is
insufficient. Calls without that positive precondition retain the stricter
Navigation-list requirement, including failure compensation invoked before a
detail label was confirmed.
