# Elite Dangerous target distance text

This finite internal Action captures the reviewed lower-left target line at
`x=100,y=790,w=450,h=110` in the centered 1920x1080 reference coordinate
space. The Rule-declared resident `ocr/w480` profile returns unrestricted raw
OCR text and confidence so the unit remains observable.

The Action does not infer a missing distance, correct malformed characters,
compare against the docking threshold, or retain a previous frame. When the
Target panel is absent or cockpit panel focus moves the HUD outside this fixed
region, its raw evidence is allowed to be empty; the classifier owns the
explicit `UNKNOWN` result.
