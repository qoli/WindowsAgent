# Cyberpunk 2077 click at current pointer position

This finite Action reads the current Windows pointer position and emits exactly
one 40 ms left click without moving the pointer. The runtime revalidates that
`Cyberpunk2077.exe` remains foreground before injection and reports the exact
primary-screen position used.

The Action accepts no inputs. It fails explicitly when the pointer cannot be
read, lies outside the primary display, the foreground changes, or input
injection fails. Completion proves injection only; callers must independently
verify any application-visible result.
