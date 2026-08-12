# Open Commodity Market

This interruptible linear Streaming Action owns the transition from the
current docked-cockpit menu to the Commodity Market in the caller's exact
`BUY` or `SELL` mode. It requires two current resident PP-OCR observations
containing all three docked menu labels before input. Repeated `DOWN` clamps
focus at `DISEMBARK`; two `UP` inputs then focus `STARPORT SERVICES`, which is
selected once. After the services transition, the Action clicks the fixed
Rule-owned Commodity Market tile at reference coordinates.

The Action never treats navigation input as success. It requires two current
header observations containing `COMMODITIES MARKET`, the exact Station name,
and either `BUY FROM MARKET` or `SELL TO MARKET`. If the current mode differs,
it clicks the fixed BUY or SELL tile and again requires two exact header
observations in the requested mode. The successful postcondition intentionally
leaves the requested market open for `trade-visible-commodity`.

Failure cleanup is registered only after the market title and Station have
been confirmed. It then uses `exit-commodity-market`; a failure before that
proof does not send blind BACK inputs. OCR ambiguity, an unexpected Station,
or a mode that does not change fails explicitly. No other Station service,
market tab, or pointer location is substituted.
