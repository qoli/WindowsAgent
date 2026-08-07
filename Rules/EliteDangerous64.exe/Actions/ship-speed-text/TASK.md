# Elite Dangerous ship speed digits

Capture the calibrated `x=1100,y=815,w=65,h=50` speed-number rectangle in the
centered 1920x1080 reference coordinate space. The resident PP-OCRv6 small
recognizer preserves the ROI aspect ratio, right-pads its 480x48 model input,
and decodes both the unrestricted character sequence and a sequence restricted
to CTC blank plus digits `0` through `9`.

The selected text is the constrained candidate. Raw and constrained text,
confidence, and their confidence margin remain in `decoding`; this Action does
not claim that a forced digit candidate is reliable. The separate ship-speed
classifier owns that fail-closed decision.
