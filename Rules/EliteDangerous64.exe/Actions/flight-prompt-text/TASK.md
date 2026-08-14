# Elite Dangerous central prompt text

This finite Action captures the reviewed `x=760,y=360,w=400,h=40` rectangle
once from the current frame. The native local crop is retained only inside the
bounded Action execution. The runtime derives reference-density RGB routes
from that same capture, and the Rule-declared resident `ocr/w480` profile
scales each selected route to the model's 480x48 input.

The explicit performance-first route contract is:

1. Run `REFERENCE_RAW_RGB`. An accepted decision stops immediately.
2. Only when the pure `elite-dangerous/flight-status-classifier` returns
   `UNKNOWN`, measure the declared lower-half native RGB Gate.
3. Only a Gate-positive frame runs `REFERENCE_BOTTOM_HALF_RGB`. That route may
   recover only `AUTO_DOCK`, `AUTO_LAUNCH`, or `FSD_CHARGING`.
4. `FSD_CHARGING` additionally requires agreement from
   `REFERENCE_CENTER_BAND_RGB`; disagreement remains `UNKNOWN`.

The Gate uses the manifest-declared orange-opponent and horizontal-edge
thresholds. They are Elite Rule data. Core owns only same-capture derivation,
route execution, and evidence. The classifier remains the single owner of the
phrase catalog and the `0.30` confidence, `0.60` similarity, and `0.10` margin
Gates.

The output preserves the selected raw OCR text plus every executed route,
decision output, Gate value, transition, selected route, terminal reason,
capture provenance, pinned model/provider identity, and timing. Capture,
resize, model, worker, schema, or decision-Action failure is terminal; none is
converted to `UNKNOWN` or another route. This Action does not compare multiple
frames, append an event, or issue game input.
