# Cyberpunk 2077 live operation

Use only Actions returned by this Rule's live catalog while
`Cyberpunk2077.exe` is the freshly observed foreground executable.

The pointer-click Action accepts one point in the centered 1920x1080 reference
coordinate space. Identify the exact control from a fresh capture, invoke one
click, then obtain another fresh capture to verify the visible result. Action
completion proves input injection only; it does not prove the overlay accepted
the click.

The click-current-pointer Action accepts no inputs, does not move the pointer,
and emits exactly one left click at its current primary-screen position. Use it
only when the operator has already placed the visible pointer over the intended
control.

Do not infer coordinates from an older frame, click outside the requested
control, repeat a click after an uncertain result, or treat this Rule as
authorization to change an in-game or overlay setting.
