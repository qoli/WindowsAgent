# Clear a Supercruise Assist line-of-sight obstruction

This interruptible linear Streaming Action owns the time-sensitive response to
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET`. It is independent from target
alignment: `align-visible-target` cannot solve a destination physically hidden
behind a body.

The workflow first requires two classified
`SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED` samples. It then commands 0%
minimum Supercruise throttle and registers critical 0% failure compensation.
`supercruise-line-of-sight-direction` must return one fresh `READY` direction
from the named target's dashed HSV-orange three-quarter focus frame. `UNKNOWN`
is terminal; there is no fixed pitch/yaw fallback.

The Action keeps that initial direction fixed. It issues bounded 800 ms
attitude pulses and re-reads the named target after every pulse. The projected
focus frame must move by at least two reference pixels; four no-progress pulses
fail. The controller does not reverse after the frame crosses screen centre.
It continues to a 96-pixel overshoot, providing a tangential course around the
occluding body. A diagonal direction uses one leased two-key hold and always
releases it on completion, failure, cancellation, or lease expiry.

After the overshoot, the Action commands 75% and performs no further attitude
input. It waits up to 60 seconds for two consecutive current OCR samples
without the line-of-sight Gate, then commands 0% and completes. Prompt
disappearance, not elapsed time, focus-frame topology, input success, or a
child terminal state, is the domain postcondition. Unexpected known prompts,
lost target geometry, missing progress, and a persistent Gate fail explicitly
with throttle-zero compensation.

This Action only clears the obstruction. Its caller must subsequently run
Compass coarse alignment and visible-target fine alignment, verify the original
prompt remains absent, then restore blue-zone throttle and reacquire
`SUPERCRUISE ASSIST ACTIVE`.
