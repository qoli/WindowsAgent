# Read Elite Dangerous cockpit HUD presence

Read the reviewed 120x120 reference-density right ship-hologram region and
count Elite Dangerous orange and cyan HUD pixels. This anchor remains visible
during normal flight and FSD charging without depending on the compass target
marker, which may legitimately disappear after a System jump. `PRESENT`
requires at least 250 matching HUD pixels;
otherwise the result is `ABSENT`. This is presence evidence only. A single
absent frame does not prove hyperspace, loading, arrival, or any other scene.
Temporal interpretation belongs to an owning Streaming Action.

The Action never searches another region, switches sampling, invokes OCR, or
reuses an older frame.
