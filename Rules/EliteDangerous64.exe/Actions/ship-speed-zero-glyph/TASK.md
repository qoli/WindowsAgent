# Elite Dangerous stopped-speed glyph

Read the calibrated `x=1100,y=815,w=65,h=50` reference ROI and classify only
the visual topology needed to distinguish ED's slashed zero from an ordinary
single digit. The orange mask is bounded to the speed-number band. Connected
components reject missing and multi-digit evidence. A zero candidate must be
one dense 9-13 by 15-20 pixel glyph whose diagonal stroke divides its interior
into at least two enclosed background regions.

This Action does not report speed and does not use OCR. It returns `ZERO`,
`NOT_ZERO`, or `UNKNOWN`, plus the measured component and topology evidence.
The composite `ship-speed` Action uses this state-specific evidence for
`STOPPED` and constrained OCR for non-zero values. A qualified multi-digit OCR
observation conflicts with rather than being silently overridden by `ZERO`.
