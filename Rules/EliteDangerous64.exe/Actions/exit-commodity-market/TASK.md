# Exit the Elite Dangerous Commodity Market

This finite composite Action leaves an open Commodity Market and then leaves
Starport Services. With `dialogMayBeOpen=false`, it sends two binding-resolved
`BACK` inputs separated by 900 ms. With `dialogMayBeOpen=true`, failure cleanup
sends one `BACK`, reads the current Commodity Market header, and sends an extra
`BACK` only when the market remains present before sending the final spaced
input that leaves Starport Services. This avoids both leaving a dialog open and
over-backing when failure happened before a dialog appeared.

The Action reports only the bounded input sequence. It does not claim any
screen is absent; a caller that requires a visual postcondition must observe it
after this Action returns. This is the shared normal-cleanup and failure-
compensation primitive for Commodity Market workflows, not a general UI
fallback.
