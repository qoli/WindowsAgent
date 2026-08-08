# Elite Dangerous ship attitude control

This finite Action resolves one pitch, yaw, or roll control from the active
Frontier binding preset and injects one bounded 40 ms scan-code press. It does
not decide which direction is correct and does not prove that the ship moved.

The Action is a low-level primitive for Rule-owned closed-loop flight Actions.
Every invocation revalidates the foreground game and the active binding before
injection. Missing, non-keyboard, or ambiguous bindings fail explicitly.
