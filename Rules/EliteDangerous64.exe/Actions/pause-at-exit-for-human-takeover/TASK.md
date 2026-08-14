# Pause at Exit for human takeover

Open the Elite Dangerous pause menu with the binding-resolved `UI_Back`
control, move focus to the clamped bottom `EXIT` row, and require two fresh
visual confirmations that both `RESUME` and `EXIT` are present and that the
`EXIT` row has the reviewed orange focus fill.

The Action deliberately does **not** send `SELECT`: its terminal postcondition
is the reviewed handoff screen with the game paused and `EXIT` focused so a
human can decide whether to resume or leave the current session. Missing,
partial, or ambiguous menu evidence fails explicitly.
