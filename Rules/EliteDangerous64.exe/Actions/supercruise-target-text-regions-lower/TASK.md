# Lower Supercruise target text regions

Detect current-frame target labels in the center-biased reference region
x=560–1360 and y=400–720. It starts where the wide upper y=240–400 package
ends and uses 256000 pixels, remaining below the resident PP-OCR runtime's
262144-pixel bound. The deeper vertical extent is required by live evidence:
after Compass alignment, a forward inter-system target label can still appear
near reference y=700 because the cockpit display shifts the visible flight
centre downward. The narrower horizontal span remains intentional because
Compass alignment has already brought the selected target near the horizontal
screen centre.

This Action returns raw boxes and text only. The composite position Action
requires one spatially unique requested target label across both bands.
