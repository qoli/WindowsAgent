# Observe a Supercruise line-of-sight body and suggest one outward direction

This internal finite Action is the conventional-CV observation module used by
`clear-supercruise-assist-line-of-sight`. It does not send input and does not
identify the destination. Its small interface returns only current-frame body
geometry, confidence, and an image-space direction away from the projected
body centre.

The Action captures the complete 1920×1080 reference viewport as one 256×144
reference sample. This is an explicit performance profile: geometry is
calculated in that sample and converted back to reference pixels. It does not
claim native 4K precision. The package-owned pinned DLL applies a separable
9×9 Gaussian blur, global Otsu thresholding, deterministic robust circle
fitting, angular-coverage and residual checks. The bottom cockpit quarter is
excluded from circle proposals so radar and ship holograms cannot win the body
fit.

`DETECTED` means a current-frame circle passed the geometry checks. `ABSENT`
means the valid current frame produced no accepted circle; it is not, by
itself, proof that a previously tracked body left the screen. `UNKNOWN` is
reserved for ambiguous or invalid domain evidence. Infrastructure, capture,
native-library, schema, and deadline errors remain terminal.

The Streaming caller must retain the initial direction, require outward
geometric progress, and require consecutive `ABSENT` observations after a
previously tracked body reached the viewport edge. A single observation never
authorizes completion.
