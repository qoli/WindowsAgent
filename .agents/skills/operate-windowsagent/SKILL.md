---
name: operate-windowsagent
description: "Discover, invoke, observe, test, troubleshoot, and extend the live WindowsAgent capabilities owned by the foreground executable's matched Rule. Use when Codex must capture the current Windows screen, decide whether an interaction belongs in a new finite or streaming Action, identify an Action capability gap, read Rule guidance and Action catalogs, invoke Actions, compose a bounded ephemeral Action Sequence, follow invocation events, validate a real game or app outcome, or repair an Action exposed by live evidence. Do not use Monitor or Reaction as executable or verified capabilities; their scheduler and dispatcher remain unvalidated and outside this v1 skill."
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

When current visual confirmation is required, capture after the UI or game has
settled. Do not associate a frame with an input that completed after the frame
was acquired, and do not use artifact encoding time as the acquisition time.

## Handle failure without bypassing the architecture

On failure, preserve the smallest complete evidence set:

- current foreground and Rule revision;
- Action or sequence request without secrets;
- invocation ID and last valid cursor;
- relevant domain events and structured error code;
- fresh visual or observation evidence;
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

If the task includes maintenance, repair the responsible Action or generic
runtime at its owning layer, run focused tests, deploy the exact changed Rule
or binary, refresh the live catalogs, and repeat the failed acceptance path.
Do not finish the task through high-level manual UI operations while leaving a
known Action defect unresolved. Do not add a hidden fallback to make the
workflow appear successful.

If the task is operation-only, do not infer permission to edit or deploy. Stop
at the explicit failure with evidence and a concrete repair boundary.

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
- Treat the foreground Rule document returned by `rule.agents.url` as the
  authority for game- or application-specific semantics.
