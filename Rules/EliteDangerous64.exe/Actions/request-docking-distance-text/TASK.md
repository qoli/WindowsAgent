# Elite Dangerous target distance text

This finite internal Action captures only the reviewed lower-left target
distance at `x=380,y=780,w=200,h=60` in the centered 1920x1080 reference
coordinate space. The narrow ROI excludes the target name and faction lines
before the Rule-declared resident `ocr/w480` profile returns unrestricted raw
OCR text and confidence, so the unit remains observable.

The Action does not infer a missing distance, correct malformed characters,
compare against the docking threshold, or retain a previous frame. When the
Target panel is absent or cockpit panel focus moves the HUD outside this fixed
region, its raw evidence is allowed to be empty; the classifier owns the
explicit `UNKNOWN` result.
