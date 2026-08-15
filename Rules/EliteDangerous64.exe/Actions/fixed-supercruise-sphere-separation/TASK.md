# Execute a fixed Supercruise sphere-separation manoeuvre

This internal interruptible Streaming Action owns one mechanical safety segment
shared by higher-level obstruction workflows. It does not identify the body,
inspect a destination prompt, align a target, or claim that the body is clear.

The Action first commands 0% throttle and requires current `Status.json`
evidence that the ship is in Supercruise, is not charging either FSD mode, and
is not overheating. It then requires two fresh
`supercruise-sphere-direction` observations that both return `DETECTED`, both
return `READY`, and select exactly the same attitude control. `ABSENT`,
`UNKNOWN`, disagreement, observation failure, or schema failure is terminal
while throttle remains commanded to 0%.

After direction confirmation, the Action executes exactly eight 800 ms pulses
in that fixed direction, for 6,400 ms of commanded turn time. It never
recomputes, reverses, or shortens the direction after the first two
observations. Diagonal controls use a separate vector-hold START/STOP lease for
each pulse; failure compensation owns both exact lease release and 0% throttle.

The Action then commands 100% throttle for exactly 30 seconds. Every 500 ms it
requires current Status evidence that Supercruise remains active, neither FSD
mode is charging, and overheating remains false. It finally commands 0% and
requires two further Status confirmations of the same conditions.

Completion proves only the fixed 6,400 ms outward turn, the fixed 30,000 ms
separation interval, retained Supercruise state, and commanded 0% throttle. It
does not prove a sphere edge transition, detector absence, stellar clearance,
safe FSD charging, prompt clearance, target identity, or target alignment.
