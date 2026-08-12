---
name: operate-windowsagent
description: "Discover, invoke, observe, test, troubleshoot, and extend the live WindowsAgent capabilities owned by the foreground executable's matched Rule. Use when Codex must capture the current Windows screen, decide whether an interaction belongs in a new finite or streaming Action, identify an Action capability gap, create a bounded Action that establishes a repeatable test state, read Rule guidance and Action catalogs, invoke Actions, compose a bounded ephemeral Action Sequence, follow invocation events, diagnose time-critical or safety-sensitive control behavior with Visual Log and authoritative Evidence, validate a real game or app outcome, or repair an Action exposed by live evidence. Do not use Monitor or Reaction as executable or verified capabilities; their scheduler and dispatcher remain unvalidated and outside this v1 skill."
---

# Operate WindowsAgent

Use WindowsAgent as a live, Rule-scoped capability runtime. Discover the current
contract from the running Agent, prefer the highest-level verified Action that
owns the requested task, and separate runtime completion from the real app or
game outcome.

## Preserve the v1 boundary

Treat these surfaces as usable in v1:

- fresh capture with foreground and matched-Rule navigation;
- finite Actions with terminal structured output;
- linear Streaming Actions with watch, status, and optional stop surfaces;
- bounded ephemeral Action Sequences generated from a live Rule allowlist;
- correlated Action events and fresh visual or observation evidence.

Treat Monitor and Reaction as **unverified**. Their declarations and code may
be inspected, but the registration scheduler and Reaction dispatcher have not
passed live acceptance. Do not register, start, invoke, recommend, or claim
Monitor/Reaction behavior as working. `registrableAs` proves eligibility only,
and a registration catalog entry would not by itself prove dispatch.

Do not revive the retired continuous ScreenParser event-loop direction.
ScreenParser is an on-demand finite Action unless a later, live-verified
contract explicitly supersedes this skill version.

## Start from the live runtime

1. Resolve the Agent base URL from the user's environment, supplied tooling,
   or current workspace instructions. Never invent a host, credential, token,
   or private endpoint. Remember that the default listener has no HTTP
   authentication and belongs only on a trusted network.
2. Check the current Agent status before invoking a capability.
3. Request a fresh capture through the environment's maintained capture path.
   Do not reuse an older screenshot as proof of current foreground state.
4. Require current foreground identity and `rule.status: matched`. Stop
   explicitly when foreground identity is unavailable, the wrong executable is
   active, or no Rule matches.
5. Follow the navigation URLs returned by that capture. Read
   `rule.agents.url` before taking Rule-specific action, then read
   `rule.actions.url`. Read `rule.runtimes.url` or the sequence tool URL only
   when the task needs them.

The running catalog outranks a repository file because Rule packages may be
hot-updated independently. After deployment or Rule synchronization, fetch a
new capture and catalogs before claiming the capability is available.

## Decide when an interaction must become an Action

Before composing primitives or starting a multi-step task, decide which layer
owns every intended game capability. Treat an interaction as an **Action gap**
when no existing Action owns its complete, evidence-backed postcondition.

Create or extend an Action when any of these conditions apply:

- completion requires more than one observe-decide-operate cycle;
- the result of an operation cannot be confirmed by that same bounded call;
- correct response timing is shorter than the supervising model's interaction
  cadence;
- execution must wait for the game or application to finish an animation,
  transition, movement, loading phase, or other asynchronous work;
- execution must retain prior observations, trends, counters, control
  ownership, or other state across calls;
- the capability needs a timeout, retry, debounce, cancellation, cleanup, or
  failure compensation contract;
- a stable game-level capability would otherwise require the supervising model
  to repeat or conditionally combine primitive inputs;
- the capability should be reusable by another workflow, sequence, Monitor, or
  Reaction; or
- success depends on domain evidence rather than proof that an input was sent.

Use this decisive test: if the supervising model must wake up in a later turn
to ensure the current operation completes correctly, put that responsibility
inside an Action.

Apply the same test to live setup and reproduction. If repeated testing needs
the supervising model to fly, wait, steer, open UI, or otherwise recreate a
domain precondition over several turns, create a separate bounded setup Action.
Its terminal evidence must prove the requested precondition, not a proxy such
as elapsed time, commanded throttle, requested distance, or an injected key.
Keep the setup Action independent from the capability under test so a passing
setup cannot conceal that capability's failure. Give it its own timeout,
cancellation, cleanup, and failure compensation.

Choose the smallest suitable abstraction:

- Use a finite Action for one bounded operation or observation whose terminal
  output can express its complete postcondition.
- Use a linear Streaming Action when the capability owns an asynchronous or
  repeated observation-and-operation lifecycle with explicit terminal states.
- Use an ephemeral Action Sequence only for a fixed, finite ordering of
  already-complete Actions. Do not use a sequence to hide a missing capability,
  stateful decision, or compensation contract.
- Keep long-horizon intent, target choice, and composition of independent
  capabilities with the supervising model.

For safety-sensitive or high-cadence control, keep the entire critical segment
inside one Streaming Action. Do not split a heat-limited charge, collision
avoidance maneuver, transient alignment, or similar deadline across model
turns. The Action must observe, decide, operate, cancel, and compensate at its
own cadence; the supervising model watches its events and decides only the
long-horizon goal or whether to interrupt it.

Do not create an Action merely to rename unrelated primitives or to encode a
one-off sequence with no stable capability semantics. When the domain
postcondition is not yet understood, collect evidence first instead of
inventing a success condition.

When an Action gap is found, do not improvise the missing capability by
repeated primitive calls. If the task authorizes maintenance, implement the
smallest Action at the Rule or generic runtime layer that owns the semantics,
then test, deploy, refresh the live catalog, and use it. If the task is
operation-only, stop at the gap and report the required Action contract without
editing or deploying.

## Select the capability

Prefer the highest-level existing Action whose documented postcondition owns
the requested outcome. Do not reconstruct a workflow through individual key
presses when a verified composite or Streaming Action already owns it.

Treat a composite Action as a dependency graph, not one opaque capability. If
it fails, locate the lowest child whose declared postcondition was not met and
invoke that child independently against the same current scene when doing so is
safe. A finite observation returning `UNKNOWN` while a fresh frame appears
human-readable is evidence of a classifier, ROI, or localization gap in that
child. Repair and accept the child first; do not widen the parent timeout,
weaken the parent Gate, or manually finish the workflow around it.

Examples:

- Prefer a name-driven select-and-lock workflow over manually moving UI focus.
- Prefer a docking workflow over independently issuing each docking input.
- Use a primitive Action only when no higher Action owns the goal, when
  performing a bounded diagnostic, or when the user explicitly requests the
  primitive.

Before invocation, read the Action's live input schema, execution declaration,
and Rule guidance. Supply only schema-valid inputs. Do not guess implicit
defaults, fixed physical keys, UI coordinates, foreground identity, or game
state.

## Invoke by execution type

### Finite Action

Invoke through `POST /v1/actions/invoke`. Expect HTTP `200`, terminal
`COMPLETED`, and schema-validated output.

Interpret that output narrowly. A completed key Action proves that the current
binding was resolved and injected; it does not prove the application accepted
the input, moved focus, changed state, or completed the user's goal.

### Streaming Action

Invoke through `POST /v1/actions/invoke`. Expect HTTP `202`, `RUNNING`, an
invocation ID, a watch URL, and an optional stop URL.

Follow the returned NDJSON watch stream from its cursor until exactly one
terminal state appears: `COMPLETED`, `FAILED`, or `CANCELLED`. Preserve the
invocation ID, cursor, event type, domain payload, and terminal error. Do not
infer completion because the connection was quiet, a command returned, an OSD
appeared, or one intermediate phase succeeded.

Use the stop URL only for an interruptible invocation and only when the user
requests cancellation, the task becomes unsafe to continue, or current
evidence proves the original goal is no longer valid. After stopping, wait for
the terminal cancellation record.

### Transient observations inside a Streaming Action

Treat observations that exist only during a charge, animation, focused panel,
temporary prompt, or other short-lived mode as Action-local ephemeral state.
Express their lifecycle in events when they can authorize control:

- `LIVE` means the evidence belongs to the currently verified mode;
- `CACHED_ONE_SHOT` means it may authorize at most the next bounded command;
- `EXPIRED` means no later operation may consume it.

Invalidate the observation after the authorized command, cancellation, mode
change, foreground change, or any transition that can alter its meaning. A
historical coordinate or classifier result remains diagnostic evidence, not a
durable claim about the current world. Require a fresh observation before the
next control decision.

When acquiring transient evidence is itself risky, use a bounded probe cycle:
establish a safe baseline, enter the temporary mode, acquire enough consistent
samples, leave that mode, verify the newer idle state, consume at most one
bounded command, then probe again. Do not keep the risky mode active while the
supervising model reasons. Prefer a deliberately non-harmonic sampling cadence
when a flashing HUD can phase-lock a fixed loop into repeated false absence.

For OCR- or CV-derived target localization, bind identity and geometry to the
same captured frame. Prefer an exact normalized match. If the UI can occlude or
split a known target label, allow a relaxed match only when all of these remain
bounded: the expected target was supplied explicitly, fragments come from the
same frame and intended ROI or band, their spatial ordering and separation are
plausible, and every fragment satisfies a documented prefix or confidence
rule. Return the raw fragments, bounds or reference point, match reason, and
timing. Never turn arbitrary fuzzy text similarity across unrelated boxes or
frames into control authority.

### Ephemeral Action Sequence

Fetch `/v3/rules/{rule-id}/action-sequence-tool` immediately before composing
the sequence. Use only the Action IDs and exact input schemas present in that
live declaration.

Use a sequence only for a disposable, immutable, same-Rule series of 1–20
steps. It supports ordered literal inputs, including allowlisted linear
interruptible Streaming Actions. It does not support variables, output
references, conditions, loops, retries, compensation, nesting, concurrency,
crash recovery, or persistence.

Preflight the entire sequence, then invoke
`POST /v1/action-sequences/invoke`. Follow the parent watch stream and retain
step number, Action ID, child execution identity, child output, and wrapped
child events. The first child failure stops the sequence. Sequence
`COMPLETED` proves that its children completed in order; it still does not
prove the external game or application reached the user's intended state.

## Validate the domain outcome

Use the following evidence ladder and report each reached layer separately:

1. **Transport** — the request reached the current Agent.
2. **Runtime** — the finite Action or invocation reached its declared terminal
   state.
3. **Execution** — the key, observation, child Action, or sequence step
   produced its declared output.
4. **Domain** — fresh observation events or a fresh frame prove the game or
   application accepted the effect.
5. **Goal** — the user's complete requested outcome is visibly or
   structurally confirmed.

Never collapse these layers. In particular:

- HTTP success is not Action success.
- Action success is not application acceptance.
- Sequence success is not workflow success.
- A throttle command is not observed speed.
- A selected contact is not a docking request.
- An injected jump control is not arrival in another system.
- A build, deployment, or catalog entry is not live behavioral acceptance.

Declare valid completion evidence according to the domain's state machine. A
transient HUD marker disappearing during a successful transition is not, by
itself, failure. A newer authoritative game-state transition may complete the
Action only when the package contract explicitly declares that the application
cannot make that transition without satisfying the missing visual Gate. This
is an evidence alternative, not permission to substitute a cached observation
or guessed result.

When current visual confirmation is required, capture after the UI or game has
settled. Do not associate a frame with an input that completed after the frame
was acquired, and do not use artifact encoding time as the acquisition time.

`COMPLETED` may legitimately end at a declared
`VISUAL_CONFIRMATION_REQUIRED` handoff. In that contract, runtime completion
means the Action reached its safe autonomous boundary; it does not mean the
Action visually proved the user's final goal. Preserve fields such as
`visualConfirmed: false`, require a fresh supervising-model capture for the
goal layer, and report runtime and goal evidence separately. Use this handoff
only after the time-critical controller, input lease, cleanup, and compensation
responsibilities are finished.

## Diagnose temporal Action behavior

Use the independent Visual Log and Evidence processes when Action correctness
depends on motion, transition order, control-mode changes, visual transitions
missed by sparse ad hoc captures, or behavior between supervising-model turns.
Read [`use-visual-log`](../use-visual-log/SKILL.md) before operating those
processes; it owns their lifecycle, authentication, range, and verification
rules.

Keep the three evidence roles distinct:

- Action events are authoritative for the controller's declared phase,
  measurement, command, Gate result, and reason.
- Visual Log descriptions are an untrusted timeline index for locating a
  candidate interval. Never feed Gemma descriptions back into a control loop
  or use them to claim a HUD state, angle, target identity, or Action success.
- Evidence MP4 and its manifest are the authoritative visual record. Use them
  to verify motion, application response, phase transitions, and the external
  goal while preserving every declared gap and missing slot.

Check the configured sampling cadences before relying on this method. Absence
from a 1 FPS recording or a slower Visual Log proves nothing about a visual
state that may last less than one sample interval; put time-critical detection
inside an OCR, CV, or other owning Action loop.

For a bounded diagnostic run:

1. Request a finite Evidence run long enough to cover setup, Action execution,
   and postcondition observation. Preserve its `runId`, immutable end time,
   frame count, gaps, and tap failures.
2. Wait for Evidence to reach `recording` and publish at least one current
   frame before starting Visual Log. `recording` may precede the first fresh
   frame-tap publication. If Visual Log warm-up rejects a stale frame, retain
   that failure, wait for a current frame, and retry only Visual Log; do not
   restart or extend Evidence.
3. Immediately before invoking the Action, record UTC time and the starting
   event cursor. Preserve the Action invocation ID and every event timestamp.
4. Follow the Action watch independently. Do not delay a safety compensation
   or terminal-state check while waiting for model descriptions or Evidence
   export.
5. Query Visual Log for the Action interval and use scene changes only to
   narrow candidate ranges. Add context on both sides according to actual
   sample spacing and drops.
6. Inspect a contact sheet for coarse localization, then retrieve and verify
   the authoritative Evidence range before diagnosing control behavior or
   claiming the game outcome.
7. Stop only the Visual Log session owned by the task when it no longer adds
   value. A finite Evidence run completes on its immutable deadline.

For alignment, steering, docking, launch, or other feedback Actions, correlate
the timelines to distinguish:

- no application response after an issued command;
- response on the wrong axis or in the wrong direction;
- control-law overshoot or oscillation;
- a continuous-to-pulse mode switch that happened too early or too late;
- a visual-target, compass, OCR, speed, heat, or other Gate that changed on a
  different frame than the Action assumed; and
- correct domain progress followed by runtime interruption.

Prefer a coarse-to-fine correction loop when one sensor is robust over a wide
range and another is accurate only inside a smaller visible region. Use the
coarse observation to enter the fine sensor's valid domain, use the fine
observation to converge, then re-read the original application Gate on fresh
frames with its declared debounce. Do not treat either child Action's
`COMPLETED` as proof that the original Gate cleared. Bound the number of full
correction cycles and fail with a stable persistent-Gate reason if the prompt
or condition remains.

For heat-, collision-, or resource-limited Actions, also reconstruct the
safety timeline. Emit the safety measurement, its freshness, the active phase,
the command being protected, and the exact cancellation or compensation
reason. Use phase-local ceilings only when the domain justifies them; entering
an irreversible but bounded transition may need a different Gate than searching
for alignment. A relaxed phase must have a clear entry proof, deadline, and
terminal state and must not leak back into the search phase.

After any comparatively slow OCR, CV, model, or artifact operation, refresh
the cheapest authoritative fast state before sending a cancellation or other
state-changing input. The application may have completed the transition while
the observation was running. If the newer state proves completion, record that
the domain transition won the race and do not inject a stale cancellation.

Do not ask the supervising model to repair timing by issuing ad hoc primitives
between turns. Use the timeline to repair the owning Action's observations,
telemetry, control law, timeout, or compensation. A calibration-capable
Streaming Action should emit enough structured events to reconstruct at least
`phase`, `measurement`, `command`, `gate`, and `reason`; visual prose is not a
substitute for those fields.

If Visual Log remains active after its finite Evidence producer completes,
evidence-stage drops are expected and do not invalidate earlier committed
Evidence. Report the covered interval and later drops separately. If a
watchdog, deployment, or process update overlaps the run, treat it as a
possible environment cause only after correlating process identity, service
lifecycle records, Action terminal events, and Evidence gaps. Temporal
coincidence alone is not root-cause proof.

An Action may fail at runtime after the game visibly advances or even reaches
the intended scene. Report runtime `FAILED` and the independently observed
domain progress separately; never promote visual completion into Action
`COMPLETED`.

Likewise, preserve a structured observation Action's domain `UNKNOWN` even if
Evidence appears human-readable. Report the disagreement as classifier or ROI
evidence and repair the owning Action; do not replace its output with a visual
guess or the last commanded value.

## Handle failure without bypassing the architecture

On failure, preserve the smallest complete evidence set:

- current foreground and Rule revision;
- Action or sequence request without secrets;
- invocation ID and last valid cursor;
- relevant domain events and structured error code;
- fresh visual or observation evidence;
- Visual Log session ID, relevant untrusted event sequences, and the verified
  Evidence run/range when temporal behavior is material;
- whether any input lease or compensating Action remains active.

Classify the failure before changing code:

- **environment** — wrong foreground, disconnected game, unavailable input
  context, Agent/runtime health, or network state;
- **capability contract** — invalid schema, absent Action, unsupported
  lifecycle, or deployment/catalog drift;
- **Action implementation** — incorrect ROI, classifier, Gate, control law,
  binding resolution, or cleanup;
- **domain rejection** — the Action executed but the application rejected or
  blocked the requested effect.

Keep same-provider transient retry distinct from provider fallback. A bounded
retry of an idempotent observation after a known transport interruption may be
implemented at its generic owning boundary when attempts and the final error
remain visible. It must not change capture backend, provider, ROI, algorithm,
or evidence source. Do not add Action-level retry loops merely to hide a broken
capture runtime, and never convert exhausted infrastructure retries into domain
`UNKNOWN`.

If the task includes maintenance, repair the responsible Action or generic
runtime at its owning layer, run focused tests, deploy the exact changed Rule
or binary, refresh the live catalogs, and repeat the failed acceptance path.
Do not finish the task through high-level manual UI operations while leaving a
known Action defect unresolved. Do not add a hidden fallback to make the
workflow appear successful.

Choose the narrowest deployment boundary. For Rule-owned Starlark, schemas,
manifests, coordinates, or game semantics, synchronize the changed Rule
without restarting or replacing unrelated Agent binaries. For a generic
runtime or executable change, build and deploy the owning binary through its
documented installer or updater. In both cases, verify the installed artifact
or manifest version, request a fresh capture, re-read the live Action catalog,
and rerun the exact previously failing path before claiming acceptance. A
successful copy or hot sync is deployment evidence, not behavioral evidence.

If the task is operation-only, do not infer permission to edit or deploy. Stop
at the explicit failure with evidence and a concrete repair boundary.

If Agent restart is blocked by a named incomplete capture staging transaction,
preserve the exact error and validate the exact staging path. Do not delete it
or clear the artifact store broadly. When operational recovery is authorized,
quarantine only that validated transaction recoverably, restart the installed
Agent through its documented owner, and require fresh health, foreground,
matched Rule, and catalog evidence before continuing.

## Report completion

State:

- the fresh foreground Rule and capability selected;
- invocation or sequence identity;
- runtime terminal state;
- the independent domain and goal evidence;
- failures, repairs, tests, and live redeployment checks, when applicable;
- remaining unverified behavior;
- Git commit and push state only when repository changes were requested.

Never describe Monitor or Reaction as validated in this v1 report.

## Read deeper only when needed

- Read the [repository runtime overview](../../../README.md) for the current
  HTTP surface and shipped capabilities.
- Read the [Streaming Action contract](../../../docs/design/streaming-action-runtime.md)
  before changing or diagnosing invocation lifecycle behavior.
- Read the [Ephemeral Action Sequence contract](../../../docs/design/ephemeral-action-sequence.md)
  before changing or diagnosing sequence behavior.
- Read the [Event Stream contract](../../../docs/design/event-stream-runtime.md)
  when investigating durable journal or cursor behavior.
- Read [`use-visual-log`](../use-visual-log/SKILL.md) before requesting finite
  Evidence, operating Visual Log, querying time ranges, or retrieving MP4
  evidence.
- Treat the foreground Rule document returned by `rule.agents.url` as the
  authority for game- or application-specific semantics.
