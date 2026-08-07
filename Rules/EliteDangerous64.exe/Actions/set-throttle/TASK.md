# Elite Dangerous set throttle

This finite Action sets the requested throttle to exactly `0` or `100` by
looking up `SetSpeedZero` or `SetSpeed100` in the game's currently active
binding preset. It never assumes a preset filename or physical key.

The runtime requires exactly one supported Keyboard binding for the selected
logical control and revalidates `EliteDangerous64.exe` immediately before
input injection. A missing or ambiguous active preset file, missing binding,
unsupported key name, ambiguous Keyboard bindings, or foreground drift fails
without sending a substitute key. The output names the resolved preset,
binding file, logical control, and actual key.
