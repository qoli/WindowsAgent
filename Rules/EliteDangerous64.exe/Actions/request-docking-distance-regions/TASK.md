# Elite Dangerous target distance text regions

This finite internal Action captures the reviewed horizontal lower-left HUD
search band at `x=0,y=730,w=768,h=240` in the centered 1920x1080 reference
coordinate space. The Rule-resident PP-OCRv6 small text-regions profile detects
and recognizes every visible line; the pure range classifier selects a strict
distance candidate from the returned current-frame regions.

The band covers the complete distance movement observed in the 99-frame Auto
Dock replay while reducing the detector input height from approximately 736 to
416 pixels. It remains reference-density. The Action does not use ScreenParser,
repair malformed OCR, infer a missing unit, retain prior evidence, compare the
distance with the docking threshold, or switch models or providers.
