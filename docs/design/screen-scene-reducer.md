# Screen Scene Reducer

## Status

**Retired.**

The reducer depended on the retired high-frequency ScreenParser event stream.
Its implementation and tests remain as historical evidence, but it is not in
the active Palworld Rule and its Scheduled Task is removed by the ScreenParser
preprocessor migration.

The strict deterministic reducer, durable cursor and pending-append recovery,
Palworld reactor manifest, independent Windows executable, installer, and contract
tests are implemented. Generic lifecycle discovery and additional game tuning are
deferred.

## Responsibility

`screen/scene-reducer` is an independent `reactor` between the raw
ScreenParser loop and a later ScreenVLM/Gemma reaction runtime. It consumes the
complete global event sequence, validates the configured ScreenParser stream,
persists its cursor, and emits a bounded semantic-geometry stream. It does not
perform OCR, infer page meaning, call a model, or request an action.

The reducer quantizes normalized detection geometry and compares ordered
multisets against the last emitted baseline with a deterministic Jaccard change
score. The first active frame and accumulated changes at or above the configured
threshold emit `screen.scene.changed`.
Below-threshold frames are suppressed; the configured interval emits one
`screen.scene.stable` summary. Foreground and terminal source events become
`screen.foreground.changed` and `screen.source.failure` respectively.

Each summary contains its exact input sequence range, frame count, change
score, scene signature, class counts and delta, inference provenance, reducer
runtime and thresholds, and only the configured number of highest-confidence regions. The raw detections remain
authoritative in `screenparser.*`; the reduced stream is a sustainable reader
surface, not a replacement journal.

## Durability and failure

The state file is required and identity-bound to the manifest. Every global
event must immediately follow the durable cursor, including events from other
streams. Unknown input types, malformed payloads, non-monotonic observation
time, cursor gaps, append failures, and state write failures stop the process.

Before an output append, the runtime atomically records a pending request and
its deterministic next checkpoint. After a crash, it scans committed events
for the input event's `causationId`: one match finalizes the checkpoint, no
match appends the stored request, and multiple matches fail explicitly. It
never skips a gap or silently duplicates a possibly committed output.

## Deferred

- automatic Rule reactor discovery and process lifecycle management;
- image artifact references for a ScreenVLM consumer;
- Game-specific change thresholds and region selection;
- a supervisor-owned liveness event distinct from scene observations;
- the model reaction and action-request layers.
