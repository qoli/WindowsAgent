# Cyberpunk 2077 reference-coordinate pointer click

This finite Action moves the Windows pointer to one caller-supplied point in
the centered 1920x1080 reference coordinate space and emits one left click.
The runtime revalidates that `Cyberpunk2077.exe` remains foreground before
injection and returns both reference and mapped native-screen coordinates.

Use it only after a fresh frame identifies an exact Cyberpunk 2077 or in-game
overlay control, and verify the resulting visual state with another fresh
frame. Successful injection proves neither that the control was present nor
that the application accepted the click. The Action does not accept native
coordinates, another button, double-clicks, drags, scrolling, or strings.
`holdMs` defaults to 40 and may be set from 1 through 2000.
