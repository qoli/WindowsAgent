# Elite Dangerous central prompt text

This finite Action captures the reviewed `x=760,y=360,w=400,h=40` rectangle in
the centered 1920x1080 reference coordinate space and returns a stable 400x40
RGB24 line. The Rule-declared resident `ocr/w480` runtime profile scales that
fixed 10:1 line to the model's 480x48 input.

The output is raw OCR evidence: text, confidence, capture provenance, pinned
model identity, provider identity, and timing. This Action does not correct
spelling, map text to Elite Dangerous states, compare multiple frames, append
an event, or execute another Action. A separately registered Monitor or
game-specific semantic Action owns those decisions.
