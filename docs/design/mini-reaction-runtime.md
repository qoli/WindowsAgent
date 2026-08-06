# Mini Reaction Runtime

## Status

**Draft. The ScreenParser preprocessor contract is implemented, but no
mini-model provider, model reaction loop, or action tool call is implemented.**

## Boundary

A Game-configured `reactor` is an independent process that subscribes to named
event streams, maintains its own bounded event cursor/context, calls the exact
configured ScreenVLM or Gemma-class local model, validates one structured
reaction, appends that reaction, and may request only explicitly allowed Game
actions.

The high-level model is not in this fast path. It reads the durable event
timeline containing the perception, reaction, action request, action result,
and subsequent perception.

When perception is required, the model reactor selects one exact image artifact
and invokes the ScreenParser preprocessor for that same frame. It may use the
returned geometry as short-lived model input, but ScreenParser output is not an
event and is not durable semantic state. The reactor authors a semantic event
only after the VLM has interpreted the evidence and validated a state
transition.

Invalid model output, missing referenced evidence, expired/gapped cursor,
foreground revision drift, unavailable declared action, or failed reaction
append stops the reaction. The runtime does not switch models, skip an event
gap, infer an action ID, or execute an unrecorded action.
