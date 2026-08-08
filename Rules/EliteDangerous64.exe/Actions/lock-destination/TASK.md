# Lock the focused Navigation destination

This interruptible linear Streaming Action starts on an already-open Elite
Dangerous Navigation detail card. The caller is responsible for opening the
Navigation panel, focusing the intended row, and selecting that row to open its
detail card.

Before input, the Action requires two consecutive observations that agree on
the `LOCK DESTINATION` or `UNLOCK DESTINATION` OCR label and prove the fixed
primary-action tile is filled/focused. `UNLOCK DESTINATION` returns `EXISTING`
without input. `LOCK DESTINATION` sends exactly one binding-resolved `SELECT`,
then requires two OCR observations of the named Navigation row enclosed by
angle brackets before returning `ACQUIRED`.

It never scans the Navigation list, chooses a target, changes focus, closes the
left panel, or presses an ambiguous action. A caller that opened the panel
retains responsibility for restoring the forward view.
