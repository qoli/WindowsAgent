# Align ship to the selected Station target

This interruptible linear Streaming Action aligns the ship's nose with the
currently selected target represented by the Elite Dangerous compass. It does
not select or identify a Station, approach it, request docking, or run the
docking computer. The caller must establish the intended Station target lock
before starting this Action.

The Action first commands 0% throttle, then repeatedly reads
`elite-dangerous/compass` and issues one bounded pitch or yaw press through
`elite-dangerous/ship-attitude-control`. A hollow marker is treated as a rear
hemisphere target and is driven away from the compass center until it crosses
to a solid front marker. A solid marker is driven toward the center. Completion
requires three consecutive solid samples inside the four-pixel center zone.

Every observation and command is emitted as
`action.align-station-target.update`. Explicit `stream.activity` records expose
phase transitions and control pulses to the Windows Action OSD. The Action
fails rather than guessing when the compass target is absent, its hollow/solid
topology is ambiguous, a child Action fails, or the bounded command limit is
exhausted.
