# Visual Log Runtime

## Status

**Partially landed.** Per-game prompt configuration, PC-local frame-tap input,
direct LAN oMLX requests, warm-up, independent control, failure isolation, and
event append passed live Windows acceptance. Persistent installation remains
deferred.

## Responsibility

Visual Log is an optional coarse timeline index. Gemma receives one current
Evidence frame and describes the directly visible physical scene. The result
helps a high-level model locate a likely recording interval; it is never
authoritative evidence.

The timestamp comes from the recorder's frame tap, not Gemma. Visual Log does
not compare frames, identify START or END, detect events, interpret HUD state,
infer actions, or write a gameplay narrative. Its logical entry remains:

```text
timestamp <Evidence frame time>
description <Gemma description>
```

The event marks the description `untrusted` and retains the model ID, latency,
Evidence slot, and local capture identity.

## Independent data path and lifecycle

Evidence and Visual Log own separate schedules. Evidence records continuously
at 1 FPS. On each Visual Log interval, the PC process reads the newest frame
newer than its own cursor from the configured named shared-memory tap, converts
that local BGRX frame to a bounded JPEG for model input, and sends exactly one
image to the configured oMLX LAN endpoint. It does not call the screenshot API,
the Evidence range API, or an Evidence single-frame HTTP route.

The high-level model starts and stops only a Visual Log run:

```text
GET    /v1/visual-log/status
POST   /v1/visual-log/runs
DELETE /v1/visual-log/runs/current
```

The control server is authenticated and loopback-only. Starting creates a new
session and performs the configured warm-up. Stopping cancels only that run and
leaves Evidence untouched.

An absent, stale, mismatched, or changing tap frame drops that index sample. An
invalid or low-quality Gemma answer records a Visual Log failure when
provenance is available and drops only that sample. Neither path can terminate
Evidence. No old frame, prior description, alternate model, screenshot call,
or hidden provider is substituted. The high-level model can always bypass the
index and request authoritative Evidence ranges.

## Elite Dangerous prompt

The executable config owns the prompt and sampling settings:

```text
Describe the directly visible physical scene in this single Elite Dangerous screenshot using 8-16 words. Mention the environment and large structures behind the cockpit overlay, not the gameplay situation. Ignore HUD text and do not infer actions or events.
```

Each request contains exactly one image followed by `Describe this image.`.
Thinking is disabled. The response schema permits exactly one `description`
string; an out-of-contract response is discarded without affecting Evidence.

The oMLX endpoint is operator configuration supplied with
`--model-base-url`; it is intentionally absent from public Rule config. The PC
must reach the endpoint directly over the LAN. SSH tunnelling is not part of
the runtime architecture.

## Live acceptance

On 2026-08-11 the Evidence process exposed
`Local\\WindowsAgent.Evidence.EliteDangerous.v1` while Visual Log ran as a
separate GUI process. The PC called the operator oMLX LAN endpoint directly and
Gemma committed this index entry in 1.301 seconds:

```text
timestamp 2026-08-11T15:13:18Z
description Vast, industrial interior with metallic structures, massive support beams, and bright overhead lighting.
```

The event retained model `gemma-4-e4b-it-8bit` and Evidence slot sequence 7.
During inference Evidence advanced independently from 5 to 9 frames with zero
gaps and zero tap failures. The production capture Agent remained PID 15032.

## Deferred

- persistent installer/watchdog integration;
- automatic retention policy for Evidence recordings.
