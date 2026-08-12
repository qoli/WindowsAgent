# Elite Dangerous single text-entry key

This finite Action sends exactly one allowlisted text-entry key while
`EliteDangerous64.exe` remains the revalidated foreground process. It supports
ASCII letters, digits, Space, hyphen, Backspace, and Enter for slow model-supervised
fields such as Galaxy Map System search. The caller must inspect the current UI
and focus before every sequence; successful injection does not prove that a
field accepted the character or that a route was plotted.

The mapping is literal and bounded rather than sourced from the Frontier flight
bindings because text editing is a Windows keyboard interaction. The runtime
still uses the same scan-code SendInput driver and foreground checks. It does
not accept arbitrary key names, chords, clipboard content, strings, or hidden
fallback input mechanisms.
Hyphen is explicit because exact procedural System names commonly contain it;
successful injection still does not authorize accepting a partial search
result.
