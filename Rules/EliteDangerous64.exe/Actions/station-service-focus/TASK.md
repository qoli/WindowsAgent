# Elite Dangerous station service focus

This finite CV Action reads the four docked-cockpit service tiles as one
reference-density region. It converts every tile interior to BT.601-style
grayscale luminance, then classifies the uniquely brightest tile as `REFUEL`,
`REPAIR`, `RESTOCK`, or `LAYER_SWITCH`.

The decision is relative across the four tiles and also requires an absolute
luminance floor. This keeps a grey unavailable service distinguishable when it
receives the game's bright keyboard-focus fill. Missing, dark, or insufficiently
separated evidence returns `UNKNOWN`; the Action never substitutes the last
focus or assumes Refuel.
