# Advance toward Station

Advance under one bounded positive Elite Dangerous throttle preset until the
currently displayed Station target-lock HUD distance reaches the caller's stop
distance. This Action does not measure travelled distance, obstacle distance,
or world geometry. Its only distance evidence is the current output of
`elite-dangerous/request-docking-range`.

Before applying throttle, require two mutually continuous Station HUD distance
observations. The caller must already own the intended Station target lock and
must provide a settled forward cockpit view. If the baseline is unavailable,
apply no throttle.

While moving, stop before confirming the first displayed distance at or below
`stopAtStationDistanceMeters`. Confirm the target once more only after 0%
throttle was sent. A missing or unknown distance, a discontinuity larger than
1000 metres, two trusted samples moving away, timeout, failure, or cancellation
must invoke the registered 0% throttle compensation. Only a confirmed target
distance is a successful completion suitable for the next step of an ephemeral
Action Sequence.
