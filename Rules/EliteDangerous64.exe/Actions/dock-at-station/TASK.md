# Elite Dangerous dock at station

This interruptible linear streaming Action owns the complete low-latency
docking workflow. It first closes the left panel and waits for the canonical
forward view, then treats each finite `request-docking-range` result as a
candidate in a temporal distance trend. Two readings no more than `1000m`
apart establish a track; a larger one-frame jump is rejected as an OCR outlier
and cannot admit the workflow. Two mutually continuous readings on the new
scale deliberately rebase the track. Admission needs at least three trusted
trend samples and the last two accepted samples must both be `ALLOWED`.
`UNKNOWN` remains a streamed waiting state and cannot cause movement. Three
trusted `DENIED` samples conditionally invoke `advance-toward-station` once at
75% throttle, with a 7000m stop target and a 30-second hard limit. The child
Action owns throttle-zero compensation. After it completes at 0%, this Action
discards the pre-movement trend and independently rebuilds the range Gate.
When the initial trusted samples are already `ALLOWED`, the child is never
invoked. The admitted distance is recorded once and is never sampled again
after admission.

The Action opens CONTACTS, scans at most sixteen current targets for a confirmed
`REQUEST DOCKING` row, focuses it, sends `SELECT`, and requires two consecutive
`CANCEL DOCKING` observations. A field-observed dropped SELECT can return the
focus to the Station row while Request Docking remains available. In that
specific retryable state the Action re-establishes Request Docking focus and
retries, for at most three focused submissions. It never resubmits after
`CANCEL DOCKING` is observed. A high-confidence
`DOCKING REQUEST DENIED.` notification first produces a
`REQUEST_DENIAL_PENDING` event and never causes another `SELECT`. Two current
`CANCEL DOCKING` observations override that transient notification and continue
the workflow. Two current Request Docking observations after the notification,
or exhaustion without `CANCEL DOCKING`, produce the terminal `REQUEST_DENIED`
event; failure cleanup closes the left panel. The notification is reported as
a generic game-side rejection and is never promoted into an inferred cause.
The Action then closes the panel, commands
throttle zero, and monitors `flight-status` plus Landing Gear while the game's
Docking Computer flies the ship. `WAITING_IN_QUEUE`,
`SLOW_DOWN_FOR_AUTO_DOCK`, and `AUTO_DOCK` are all valid docking-lifecycle
states. Queue or slowdown prompts do not count as disappearance of game-side
docking control; only `UNKNOWN` can advance the terminal prompt-absence Gate
after `AUTO_DOCK` was confirmed.

Completion requires that `AUTO_DOCK` was first observed twice, subsequently
became absent for five consecutive samples, and Landing Gear was `ON` for two
consecutive samples. The terminal domain phase is
`VISUAL_CONFIRMATION_REQUIRED`; it never claims the ship is docked. Missing,
ambiguous, contradictory, or unexpected evidence fails explicitly. The Action
does not reuse prior observations, infer missing values, or switch to another
sensing pipeline.

Monitoring child calls use `action.try_call`. A failed observation is emitted
as `OBSERVATION_ERROR` and does not count as disappearance of the Auto Dock
prompt or as an unknown Landing Gear sample. Three consecutive failed samples
terminate explicitly; the Action never switches capture or state providers.
