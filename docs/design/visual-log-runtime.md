# Visual Log Runtime

## Status

**Partially landed.** The strict Game config, single-frame capture adapter,
oMLX description adapter, warm-up, independent producer loop, dropped-sample
behavior, durable event append, and authenticated resident on-demand control
surface are implemented. A bounded live Windows acceptance passed against the
signed-in Elite Dangerous session; persistent installation remains deferred.

## Responsibility

The visual log is an optional, independent process that writes an untrusted
text index of individual game screenshots. Its only semantic job is to describe
the directly visible physical scene so a high-level model can locate a likely
time range. It does not compare frames, identify START or END, detect events,
interpret HUD state, or write a gameplay narrative.

The capture timestamp comes from capture metadata. Gemma does not generate or
repair it. A committed observation payload has this logical form:

```text
timestamp <capture timestamp>
description <Gemma description>
```

The event payload also marks the description `untrusted` and records model
identity, inference latency, and the exact inference-input capture ID.

## Independent lifecycles

Evidence recording, visual logging, and high-level analysis are separate
lifecycles. Starting, stopping, dropping, or crashing the visual log must not
start, stop, pause, or delete evidence recording. The visual logger requests
its own single 1080p JPEG on its own interval; it does not consume or schedule
the evidence recorder's 1 FPS timeline.

An invalid or low-quality Gemma response drops only that visual-log sample. The
runtime emits a `visual-log.failure` diagnostic when the frame provenance is
known, records the local drop, and continues at the next interval. It never
substitutes a previous description, another model, another capture profile, or
an inferred result. A capture failure without trustworthy frame provenance is
recorded only in the process log and the next interval continues.

Failure to append to the event journal terminates only the visual-log process,
because it can no longer assert that its output is durable. It has no control
path to the evidence recorder. The high-level model's worst-case recovery is
to ignore the missing fast log and request the complete evidence range.

Cancelling a run while capture, warm-up, or model inference is in flight is
normal control flow. The run reaches `stopped` without inventing a failure
event or a dropped sample for the cancelled operation.

## Elite Dangerous prompt

The Game config owns the exact prompt and sampling settings. The selected
experimental baseline is:

```text
Describe the directly visible physical scene in this single Elite Dangerous screenshot using 8-16 words. Mention the environment and large structures behind the cockpit overlay, not the gameplay situation. Ignore HUD text and do not infer actions or events.
```

Each request contains exactly one image followed by `Describe this image.`.
Thinking is disabled and strict JSON permits exactly one `description` string.

## Current seam

The deep module interface is `visuallog.Runner`: warm up, observe once, or own
the timed loop. Capture, oMLX, and event append are internal adapters. The
Windows command supplies credentials and endpoints through absolute paths and
flags; no credential or private endpoint belongs to the public Game config.

The process control adapter listens only on an explicit loopback IP. Its
authenticated interface is intentionally small:

```text
GET    /v1/visual-log/status
POST   /v1/visual-log/runs
DELETE /v1/visual-log/runs/current
```

Starting a run creates a new producer session, warms the configured model, and
then enters the module-owned loop. Stopping cancels only that run; the process
returns to an idle state and has no evidence-recorder control interface.

## Live acceptance

The 2026-08-11 Windows acceptance used a fresh matched
`EliteDangerous64.exe` foreground, the installed capture Agent, the configured
Gemma model on an operator-supplied oMLX endpoint, and an isolated current
event-journal process so the active production journal did not need a restart.

- warm-up reached `active` in 2.391 seconds;
- three observations committed with zero failures and zero dropped samples;
- model latency was 1.265, 1.481, and 1.316 seconds;
- the descriptions correctly located the inspected 1080p inputs as a deep-space
  starfield viewed through the cockpit;
- a range request with limit two paged the three events as two plus one while
  preserving cursor order;
- stop returned `stopping`, reached `stopped`, and produced no later sequence;
- the installed capture and production event processes retained their process
  identities throughout the run.

A separate controlled bad-output run required 63 through 64 words while
leaving the normal 8 through 16 word prompt unchanged. Warm-up and two
published attempts were dropped at the model boundary; two
`visual-log.failure` events retained their capture IDs, no observation was
committed, the producer remained active, and stop still reached `stopped`.
This test configuration and its temporary Scheduled Task were removed after
acceptance.

The production event service was deliberately not upgraded during acceptance
because a live Streaming Action was actively writing to it. Therefore the
current machine's production `/v1/events/range` deployment remains an operator
rollout step even though the endpoint implementation passed in the isolated
acceptance journal.

## Deferred

- independent 1 FPS evidence recorder and time-range evidence download;
- installer/watchdog integration;
- rollout of the current event-range endpoint to machines still running an
  older event-stream executable.
