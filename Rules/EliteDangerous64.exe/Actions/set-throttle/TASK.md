# Elite Dangerous set throttle

This finite Action sets the requested throttle to exactly `-100`, `0`, or `100` by
looking up `SetSpeedMinus100`, `SetSpeedZero`, or `SetSpeed100` in the game's currently active
binding preset. It never assumes a preset filename or physical key.

The runtime requires exactly one supported Keyboard binding for the selected
logical control and revalidates `EliteDangerous64.exe` immediately before
input injection. A missing or ambiguous active preset file, missing binding,
unsupported key name, ambiguous Keyboard bindings, or foreground drift fails
without sending a substitute key. The output names the resolved preset,
binding file, logical control, actual key, scan-code backend, scan code,
extended-key flag, and the manifest-declared 40 millisecond hold time.
