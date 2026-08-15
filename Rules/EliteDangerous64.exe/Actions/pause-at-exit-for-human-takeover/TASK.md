# Replay the reviewed safe-exit key structure for human takeover

After the owning flight workflow has detected the near-orbit safety condition
and commanded 0% throttle, replay the reviewed binding-resolved Elite Dangerous
menu sequence exactly:

1. `PAUSE` once;
2. `DOWN` five times;
3. `SELECT` once to choose the first-level `EXIT` item; and
4. `SELECT` once to choose the default-focused second-level
   `EXIT TO MAIN MENU` card.

The Action deliberately performs no OCR, CV, menu observation, focus
classification, or post-exit main-menu verification. The game menu structure
and its default focus are the caller-owned precondition. It is not resumable
from an intermediate menu and must only be called once from the owning
near-orbit branch immediately after that branch commands 0% throttle.

All controls are resolved through `elite-dangerous/ui-control`; no literal key,
`BACK`, alternate binding, provider, or observation fallback exists. The fixed
timing is 750 ms after `PAUSE`, 120 ms after each `DOWN`, 750 ms between the two
`SELECT` commands, and 1000 ms after the final `SELECT`. Any child input failure
is terminal and prevents later sequence steps.

Completion proves only that all eight binding-resolved commands were injected
in the reviewed order. It does not claim that the game accepted a menu
transition, reached the main menu, paused simulation, or established any
visual postcondition. Independent fresh evidence remains required when a
supervisor needs to assert the resulting screen.
