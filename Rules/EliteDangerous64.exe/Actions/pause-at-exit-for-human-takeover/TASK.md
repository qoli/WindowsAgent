# Exit flight to the main menu for human takeover

Safely leave the current Elite Dangerous flight session after an owning
workflow has already commanded 0% throttle. Open the in-game pause menu with
the binding-resolved Frontier `Pause` control, move focus to `EXIT`, and require
two consecutive fresh observations of exact `RESUME` plus `EXIT` text and the
reviewed orange focus fill before selecting it once.

The first `SELECT` is not a terminal postcondition. It opens a second,
independently observed destination menu containing exact `EXIT TO MAIN MENU`
and `QUIT TO DESKTOP` labels. The Action requires two fresh observations that
`EXIT TO MAIN MENU` is the focused card while `QUIT TO DESKTOP` is explicitly
not focused before sending the second and final `SELECT`. Simultaneous orange
focus evidence on both cards is ambiguous and cannot authorize input. It never
selects `QUIT TO DESKTOP`, never infers the destination from a missing pause
menu, and never sends a blind second select.

After the second select, black frames, loading frames, `ABSENT`, and `UNKNOWN`
are transition evidence only. Completion requires two consecutive fresh
observations of the non-flight main menu: exact `CONTINUE`, no `RESUME`, and at
least two exact anchors from `SOCIAL`, `GAME EXTRAS`, `OPTIONS`, and
`HELP AND INFO`. This bounded verification may wait up to 120 fresh samples so
normal logout/loading latency does not become a false success.

The first-level EXIT focus threshold remains `0.05`; live 3840x2160 WGC
evidence measured focused values around `0.0696` and unfocused values at zero.
The second-level focused-card background uses a separate `0.20` orange-fill
Gate subordinate to both exact destination labels. These thresholds are not
fallbacks and neither OCR identity nor focus evidence authorizes control alone.

`UI_Back` is deliberately not used to open the pause menu: it owns in-panel
back navigation. There is no physical-key fallback for `Pause` or `SELECT`.
Pause is a toggle, so it is sent at most once and only after a fresh `ABSENT`
observation. If the Action restarts while the reviewed second-level destination
menu is already open, two fresh exclusive `EXIT TO MAIN MENU` focus observations
may resume at the second select without toggling Pause or replaying the first
select. Any other initial `UNKNOWN`, incomplete destination menu, unstable
focus, or missing main-menu postcondition fails explicitly without repeating
either select.

The pause menu wraps. Navigation therefore sends one `DOWN` at a time and
observes after every step, stopping at the first confirmed EXIT focus. Six
steps cover one reviewed menu cycle.
