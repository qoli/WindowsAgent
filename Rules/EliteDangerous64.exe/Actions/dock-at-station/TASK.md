# Elite Dangerous dock at station

This interruptible linear streaming Action owns the complete low-latency
docking workflow. It first closes the left panel and waits for the canonical
forward view, then admits the task only when the finite
`request-docking-range` Action returns `ALLOWED`. That distance is recorded
once and is never sampled again after admission.

The Action opens CONTACTS, scans at most sixteen current targets for a confirmed
`REQUEST DOCKING` row, focuses it, sends `SELECT` exactly once, and requires two
consecutive `CANCEL DOCKING` observations. It then closes the panel, commands
throttle zero, and monitors `flight-status` plus Landing Gear while the game's
Docking Computer flies the ship.

Completion requires that `AUTO_DOCK` was first observed twice, subsequently
became absent for five consecutive samples, and Landing Gear was `ON` for two
consecutive samples. The terminal domain phase is
`VISUAL_CONFIRMATION_REQUIRED`; it never claims the ship is docked. Missing,
ambiguous, contradictory, or unexpected evidence fails explicitly. The Action
does not reuse prior observations, infer missing values, resend `SELECT`, or
switch to another sensing pipeline.

Monitoring child calls use `action.try_call`. A failed observation is emitted
as `OBSERVATION_ERROR` and does not count as disappearance of the Auto Dock
prompt or as an unknown Landing Gear sample. Three consecutive failed samples
terminate explicitly; the Action never switches capture or state providers.
