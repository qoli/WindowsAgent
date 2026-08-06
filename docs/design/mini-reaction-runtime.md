# Mini Reaction Runtime

## Status

**Draft. The ScreenParser Action contract is implemented, but no
mini-model provider, model reaction loop, or action tool call is implemented.**

## Boundary

A Reaction registration subscribes to a named event selector and references
one Action that explicitly declares `reaction` in `registrableAs`. A future
reaction runtime maintains its own bounded event cursor/context, calls the
exact configured ScreenVLM or Gemma-class local model when that is the Action's
runtime, validates one structured reaction, and may request only declared Game
Actions.

The high-level model is not in this fast path. It reads the durable event
timeline containing the perception, reaction, action request, action result,
and subsequent perception.

When perception is required, the reaction runtime selects one exact image artifact
and invokes the ScreenParser Action for that same frame. It may use the
returned geometry as short-lived model input, but ScreenParser output is not an
event and is not durable semantic state. The reactor authors a semantic event
only after the VLM has interpreted the evidence and validated a state
transition.

Invalid model output, missing referenced evidence, expired/gapped cursor,
foreground revision drift, unavailable declared action, or failed reaction
append stops the reaction. The runtime does not switch models, skip an event
gap, infer an action ID, or execute an unrecorded action.
