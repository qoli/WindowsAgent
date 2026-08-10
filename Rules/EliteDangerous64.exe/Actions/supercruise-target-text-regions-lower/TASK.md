# Lower Supercruise target text regions

Detect current-frame target labels in the center-biased reference region
x=560–1360 and y=380–600. Together with the wide upper y=240–400 package, this
preserves a 20-pixel vertical overlap while keeping each resident PP-OCR
request below the 262144-pixel runtime bound. The narrower horizontal span is
intentional: this lower band is needed after Compass alignment has already
brought the selected target close to screen centre.

This Action returns raw boxes and text only. The composite position Action
requires one spatially unique requested target label across both bands.
