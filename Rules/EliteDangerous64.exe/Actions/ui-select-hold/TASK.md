# Elite Dangerous held UI select

This finite Action resolves Frontier's current `UI_Select` keyboard binding and
holds it for the caller-supplied bounded duration. It exists for UI controls
whose visible contract explicitly says `(HOLD)`, such as Galaxy Map
`PLOT ROUTE`; the ordinary `ui-control` Action remains the correct primitive
for a normal press.

Completion proves only the resolved key-down/key-up injection. The owning
Action must first establish the focused UI target and then verify the resulting
domain postcondition. It must not use a held select as a blind substitute for a
missing focus or exact-name Gate.
