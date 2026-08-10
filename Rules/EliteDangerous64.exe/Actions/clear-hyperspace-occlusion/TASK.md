# Clear a hyperspace stellar obstruction

This interruptible linear Streaming Action owns the exceptional case where the
selected hyperspace destination projects through the local arrival star. It
stops the ship, consumes the directional CV Action, and turns away while emitting
coverage deltas. A `CLEAR` frame captured during a turn is not alignment: after
all attitude input stops, the Action waits 1.5 seconds and requires three
current no-input `CLEAR` observations. If centroid
direction is unavailable, one bounded Pitch Up probe is measured; improving
coverage keeps that direction, worsening coverage reverses it.

Landing Gear and Cargo Scoop must be visually OFF. After Mass Lock is OFF, the
Action preserves the verified away-from-star heading and builds 120 seconds of
normal-space separation at 100%, requires at least two current non-zero speed
observations among the four samples at five through eight seconds (exact OCR
movement or the independent non-zero glyph topology), samples visual heat every
five seconds, and stops before charge if a
known value reaches 75%. It then stops and requires
three visual heat readings to be known and at or below 60% before charging.
After a failed invocation whose durable stream proves the complete 120-second
escape, 0% compensation, and safe heat, a supervising model may explicitly set
`normalSpaceSeparationConfirmed=true` to resume from that checkpoint. The
default is false; the Action never infers completion from command history.
When Mass Lock is still ON,
normal-space 100% is allowed only while the Action owns critical 0%
compensation and is bounded by current Status flags. Once Mass Lock is OFF, it
invokes the dedicated `Supercruise` binding, never the combined hyperspace
control. Entry uses the official `Status.json` flags for Supercruise, FSD
Charging, FSD Cooldown, Over Heating, and hyperspace-versus-Supercruise charge
type. A Status observation allows at most three bounded launcher attempts and
never substitutes stale evidence. Alignment is never attempted after charging
begins. An unsafe flag or an
fifteen-second entry timeout cancels the charge and verifies that charging ends.
During charging the same fast visual heat Action is sampled every second; 90%
also cancels immediately, before the Status overheat bit is expected.

The ship then travels tangentially at 100% Supercruise for 30 seconds while
periodically confirming both the Supercruise Status flag and non-blocking forward view.
It finishes at the minimum 0% Supercruise throttle with current `CLEAR`
evidence. Failure or cancellation sends 0%. It does not align or trigger the
hyperspace jump; the owning one-hop workflow must realign and recheck the
forward obstruction afterward.
