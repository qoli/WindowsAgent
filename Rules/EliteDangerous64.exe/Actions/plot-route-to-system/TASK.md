# Plot an exact Galaxy Map route

This interruptible linear Streaming Action owns exact System route creation
from a forward cockpit view or an already-open Galaxy Map. It first accepts an already plotted route only
when `NavRoute.json` validates to the same complete destination. Otherwise it
requires Galaxy Map to be absent, opens it, confirms the map title, focuses the
fixed search field, clears up to the maximum permitted name length with bounded
Backspace inputs, enters every character of the complete System name, and
requires an exact normalized suggestion row below the search field. It clicks
only that exact OCR box, requires the selected System information panel to
repeat the same exact name twice, holds the binding-resolved `UI_Select` on the
visible `(HOLD) PLOT ROUTE` state, and validates the resulting `NavRoute.json`
against the requested System and jump limit.

If the map was already open, the Action takes ownership of its close
compensation before touching the field. On success it closes Galaxy Map and requires two current observations with no
Galaxy Map title. When it opened the map it registers the same close operation
as failure compensation. Partial names, fuzzy suggestions, unsupported text
characters, ambiguous exact suggestion boxes, a different selected System,
an unchanged or mismatched route, an excessive hop count, and an un-restored
forward view fail explicitly. It never substitutes another suggestion, route,
destination, text provider, or input backend.
