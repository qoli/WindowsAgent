# Read Elite Dangerous ship status indicators

Read the fixed lower-right cockpit status region in the centered 1920x1080
reference coordinate space using reference-density sampling. Locate the
three-box status group shared by `MASS LOCKED`, `LANDING GEAR`, and
`CARGO SCOOP`, then classify all three rows independently. A cyan filled box
is `ON`; an orange hollow box is `OFF`. If the complete three-row structure
cannot be established, all three states are `UNKNOWN` and their `on` fields
are null.

This package does not use OCR. The label is static; the indicator illumination
is the state evidence. A malformed Observer response still fails explicitly;
an unavailable visual state remains visible as `UNKNOWN`. State transitions
require previous observations and belong to a separately registered Monitor.
