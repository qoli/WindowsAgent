# WindowsAgent Rule and Action Engineering Contract

This file governs development below `Rules/`. It inherits the repository-wide
architecture, security, validation, Git, and deployment rules from
`../AGENTS.md`.

Its primary subject is how a high-level execution Agent turns game or
application intelligence into Rule-owned Actions. It also explains how each
executable Rule's `AGENTS.md` participates in live operation, but it does not
own, review, prescribe, or duplicate the executable-specific contents of those
documents.

## Two-Layer Intelligence Model

WindowsAgent separates intelligence by response time and responsibility.

### High-level execution Agent

The high-level Agent works at model-turn latency, commonly around ten seconds
once reasoning, tool calls, capture, transport, and response time are included.
It owns:

- user intent, target choice, and long-horizon task planning;
- discovery of the current foreground Rule and its live capabilities;
- selection and composition of the highest-level verified Actions;
- recognition of capability gaps;
- evidence collection and failure analysis;
- design, implementation, testing, deployment, and repair of Actions when the
  task authorizes maintenance;
- supervision of running Actions through their durable streaming evidence; and
- independent confirmation of domain and user-goal outcomes.

The high-level Agent must not act as a millisecond- or subsecond game-control
loop. Repeatedly alternating one screenshot, one model turn, and one primitive
input is too slow to own movement, aiming, UI transitions, transient hazards,
or any other time-sensitive feedback process.

### Action fast-response layer

An Action is the local executable game capability. It owns the behavior that
must continue correctly between high-level Agent turns, including:

- bounded observation and input;
- repeated observe-decide-operate cycles;
- animation, loading, movement, or state-transition waiting;
- retained phases, observations, counters, trends, and control ownership;
- timing, debounce, bounded retry, and timeout policy;
- cancellation and input release;
- cleanup and failure compensation; and
- evidence-backed terminal postconditions.

The intended control flow is:

```text
high-level Agent understands the goal
    -> discovers an existing Action or identifies an Action gap
    -> designs, builds, tests, and deploys the owning Action when authorized
    -> invokes the Action through the live Rule
    -> observes its streaming evidence and terminal state
    -> verifies the independent domain and user-goal outcome
```

The high-level Agent provides adaptable intelligence. Actions are the compiled,
bounded, observable form of that intelligence at gameplay speed.

## Action Ownership Test

Before composing primitive inputs or beginning a multi-step interaction, decide
which layer owns the complete postcondition.

An **Action gap** exists when no current Action owns the requested capability's
complete, evidence-backed result. Create or extend an Action when any of these
conditions apply:

- completion needs more than one observe-decide-operate cycle;
- an operation cannot confirm its own result in the same bounded call;
- correct reaction time is shorter than the high-level Agent's turn cadence;
- execution must wait for an animation, transition, movement, loading phase, or
  external state change;
- prior observations, trends, counters, leases, or other state must survive
  across samples;
- correctness needs a timeout, bounded retry, debounce, cancellation, cleanup,
  input release, or compensation contract;
- a stable domain capability would otherwise require repeated or conditional
  primitive calls by the high-level Agent;
- another workflow or sequence should reuse the capability; or
- success depends on observed domain evidence rather than proof that an input
  was sent.

Use this decisive rule:

> If the high-level Agent must wake in a later model turn to ensure that the
> current operation completes correctly, that responsibility belongs inside an
> Action.

When operation alone was requested, identifying a gap does not authorize source
changes or deployment. Report the missing Action contract. When maintenance is
authorized, implement the smallest Action that owns the semantics, validate it,
deploy the exact changed Rule or runtime, refresh the live Rule catalogs, and
repeat the failed acceptance path.

## How to Split Actions

Split at a stable domain capability with one clear owner and one verifiable
postcondition. Do not split by source-file size, number of key presses, or
convenience of the current task.

Use these layers deliberately:

1. **Primitive input Action** — performs one bounded key, hold, throttle,
   pointer, or other physical operation. Completion proves execution only, not
   that the game accepted the effect.
2. **Finite observation Action** — obtains one bounded current observation and
   returns structured evidence without retained temporal state.
3. **Finite composite Action** — combines finite Actions into one bounded
   semantic result whose complete postcondition can still be returned by the
   same call.
4. **Linear Streaming Action** — owns an asynchronous or repeated
   observation-and-control lifecycle with explicit phases, interruption,
   cleanup, and terminal evidence.
5. **High-level task** — retains long-horizon intent, strategy, target choice,
   and composition of independent capabilities in the execution Agent rather
   than one oversized Action.

A useful Action resembles a reusable game skill. A raw key press is normally
too small to own a domain result; completing a whole campaign or open-ended
strategy is too large to be one bounded Action.

Prefer the highest-level existing Action whose declared postcondition owns the
goal. Do not reconstruct it from primitives. An Ephemeral Action Sequence is
appropriate only for a fixed, disposable ordering of already-complete Actions.
It must not hide a missing feedback loop, stateful decision, timeout, retry,
cleanup, compensation, or domain postcondition.

Avoid oversized Actions that combine unrelated goals. A well-bounded Action
should have:

- one stable capability identity;
- explicit inputs chosen by the caller;
- one owned domain postcondition;
- bounded resources and time;
- explicit cancellation and cleanup when it can remain active;
- structured failure modes; and
- evidence sufficient to diagnose and improve that capability independently.

## Designing a New Action

Design the contract before choosing implementation details.

1. **Bound the capability.** State the exact domain operation or observation,
   the caller-owned inputs, preconditions, success condition, unsupported scope,
   and privacy boundary.
2. **Define the evidence.** Identify which current observations prove progress,
   rejection, safety, and completion. Keep commanded state separate from
   independently observed state.
3. **Choose the execution type.** Use finite return completion only when one
   bounded call owns the complete postcondition. Use a linear Streaming Action
   for repeated or asynchronous control. Keep open-ended intent with the
   high-level Agent.
4. **Choose the owning runtime.** Select the runtime whose loader and execution
   contract match the capability. Different runtimes use different manifest
   schemas; never force every package into one template.
5. **Find the dependency seam.** Reuse same-Rule Actions whose postconditions
   already match the required child capabilities. Do not call across Rules,
   duplicate an existing classifier or input implementation, or substitute a
   lower primitive for a failed semantic child.
6. **Specify failure ownership.** Define terminal infrastructure failures,
   domain `UNKNOWN`, bounded transient errors, retry eligibility, cancellation,
   cleanup, and compensation before implementing the happy path.
7. **Define schemas and events.** Close and bound inputs, outputs, and events.
   Preserve provenance, phase, decision, and evidence needed by another Agent to
   understand execution without reverse-engineering logs or code.
8. **Implement at the owning layer.** Game-specific coordinates, bindings,
   classifiers, state machines, and source selection remain in the Rule Action.
   Generic execution belongs in Core only when it is capability-neutral and
   reusable across Rules.
9. **Test negative paths first-class.** Validate contract loading, schemas,
   dependencies, state transitions, timeouts, cancellation, cleanup,
   compensation, and evidence contradictions, not only successful output.
10. **Validate live behavior.** Build, deploy the exact package/runtime, obtain
    a fresh matched capture and live catalogs, invoke the Action, follow its
    events, and independently verify the domain and user-goal postconditions.

Do not invent a success condition when the game behavior is not yet understood.
Collect targeted evidence first, then define the smallest stable Action.

## Streaming Evidence Contract

Streaming events are the high-level Agent's primary view into a running fast
Action. They must form a bounded, structured, causally ordered execution
timeline rather than an unstructured debug transcript.

A Streaming Action should expose, when relevant:

- invocation, Rule, Action, correlation, and child execution identity;
- current phase and phase transition;
- fresh observation and its source, capture time, model/provider, or file
  provenance;
- classifier result, uncertainty, and contradictory or missing evidence;
- Gate decision and the reason it passed, waited, or failed;
- requested control, completed command, and retained control ownership;
- explicit separation of commanded and observed state;
- counters, trends, sample cadence, elapsed time, and remaining bounds;
- skipped transient samples and the exact bounded policy authorizing them;
- registered, executed, failed, or cleared cleanup and compensation; and
- terminal postcondition evidence or the smallest complete structured failure.

Emit semantically useful transitions and samples. Do not flood the durable log
with raw pixels, private content, redundant unchanged state, or prose that
cannot be validated. Reference bounded evidence artifacts when later review
requires them.

Streaming completion is not self-authenticating. The terminal state must be
supported by the Action's declared postcondition, and any goal requiring an
external visual or structural confirmation still needs fresh independent
evidence after the relevant state has settled.

Preserve these acceptance layers separately:

1. **Transport** — the request reached the current Agent.
2. **Runtime** — the Action reached a declared terminal state.
3. **Execution** — its observation, command, or child capability produced the
   declared result.
4. **Domain** — current evidence proves the game or application accepted it.
5. **Goal** — the user's complete requested outcome is confirmed.

HTTP success, input injection, Action completion, sequence completion, domain
acceptance, and user-goal success are not interchangeable claims.

## Rule Plugin Contract

Each executable-scoped Rule is one independently distributed plugin:

```text
Rules/<CanonicalExecutable.exe>/
|-- rule.json
|-- AGENTS.md
`-- Actions/
    `-- <action-package>/
```

`rule.json` is the executable registry. It owns runtime profiles, Action IDs and
paths, execution declarations, registration eligibility, the Ephemeral Action
Sequence allowlist, and explicit registrations. A directory below `Actions/`
is not a capability unless the current `rule.json` declares it.

Action IDs are global capability identity and must remain canonical and unique
across Rules. Action paths stay below the owning Rule's `Actions/` directory.
Every Action explicitly declares return or stream completion and an explicit
`registrableAs` list, including an empty list.

An omitted `exposure` is `public`. `exposure: "internal"` keeps an
implementation Action available to same-Rule composite and streaming child
calls while excluding it from public catalogs, direct invocation, v1 Script
projection, registrations, and Ephemeral Action Sequences. Internal Actions
must declare an empty `registrableAs` list.

`registrableAs` grants eligibility only. Monitor scheduling and Reaction
subscription/dispatch have not passed the required live acceptance and are not
usable capabilities under the current v1 operating contract. Do not create,
start, recommend, or report them as working. An empty `registrations` object
means every Action remains on-demand.

Runtime profiles configure lifecycle for a runtime used by Actions. Residency
does not create an Action, registration, timer, event producer, or permission to
invoke anything.

Use the loader and contract owned by the declared runtime. Observation,
composite/streaming Starlark, key input, pointer input, PP-OCR, and external
inference runtimes intentionally have different manifest shapes and validation
paths. A Rule catalog declaration alone does not prove that Core owns the
runtime, that the unified launcher supports it, or that it passed live
acceptance.

## How Executable AGENTS.md Works

`Rules/<Executable.exe>/AGENTS.md` is a required, non-empty member of its Rule
plugin and is served as live Rule guidance. Its executable-specific content is
owned outside this file's scope. Do not inspect, review, edit, reformat,
summarize, copy, or use it as a template unless the user explicitly names that
executable Rule document as the task target.

During live operation, the high-level Agent discovers it through this exact
chain:

1. resolve the current Agent origin from the authorized environment;
2. check current Agent status;
3. request a fresh capture through the maintained capture path;
4. require current foreground identity and `rule.status: matched`;
5. follow the exact `rule.agents.url` returned by that capture;
6. read that live document before taking Rule-specific action;
7. then follow `rule.actions.url`, and read the runtime or sequence-tool catalog
   only when the task needs it.

Never select Rule guidance from a window title, executable path substring,
visible content, game-name guess, repository search, or an old screenshot. Do
not invent a host or replace the returned URL with Mac `localhost`.

The installed live document and catalogs outrank repository files during
operation because Rule plugins may be synchronized independently. They are
served without caching, and a later request sees a completed plugin replacement
without restarting WindowsAgent. After any Rule deployment or synchronization,
obtain a new capture, confirm the matched foreground Rule again, and refetch its
AGENTS and Action catalogs before claiming the changed capability is available.

`Rules/AGENTS.md` is a source-development contract inherited by coding Agents in
this repository. It is not copied into an executable Rule plugin and is not
served by `/v1/rules/{rule-id}/AGENTS.md`. It must never override or impersonate
the live executable-specific document.

Use `.agents/skills/develop-windowsagent-rule/SKILL.md` as the workflow for
designing, discovering, invoking, troubleshooting, and validating Rule-owned
Actions. This file remains the source-development contract; the skill applies
it to Action engineering and live operation. Use
`.agents/skills/maintain-windowsagent-runtime/SKILL.md` for generic runtime work;
that workflow must not modify this Rule layer or remove its workarounds.

## Failure and Fallback Policy

Failure must remain visible at the layer that owns it.

- Do not silently change Rule, Action, source, process, file, save slot, model,
  precision, provider, runtime, binding, decoder, coordinate strategy, or
  algorithm.
- Infrastructure, protocol, permission, schema, deadline, artifact, process,
  and dependency failures are terminal unless an explicit owning contract
  narrowly classifies a bounded recovery.
- Domain `UNKNOWN` represents an owning classifier's valid insufficient or
  ambiguous evidence. It must not absorb infrastructure failure or be replaced
  with cached, commanded, guessed, or previous state.
- Multiple observation sources are allowed only when their order and transition
  rules are explicit in the package task, implementation, schemas, tests, and
  output attempts.
- A failed Action must be repaired at its owning layer. Do not complete the goal
  through improvised high-level UI control while leaving the known defect in
  place.

## Validation and Deployment

Inspect the worktree first and preserve unrelated Rule and Core changes. Validate
the exact runtime and package type instead of assuming one universal Action
format.

The baseline Rule checks are:

```bash
git diff --check
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
./scripts/build-windows-capture-agent.sh
```

Add targeted loader, schema, dependency, behavior, event, cancellation,
compensation, and negative-path tests for the changed Action. Run the runtime's
own contract tests and publisher checks for external inference packages.

Runtime behavior must be validated inside the signed-in interactive Windows
session. Deployment proof requires more than copying files:

1. synchronize or install the exact authorized Rule/runtime;
2. verify the transactional replacement completed without an unintended task
   restart;
3. request a fresh capture and confirm the expected matched Rule;
4. refetch the live AGENTS, Action, runtime, and sequence catalogs needed by the
   task;
5. invoke the changed Action and follow its complete terminal evidence; and
6. verify the independent domain and user-goal postconditions.

Do not publish or commit screenshots, save files, memory contents, OCR regions
or results, event journals, credentials, tokens, private endpoints, host names,
local paths, account identifiers, or raw runtime logs. Keep validation evidence
privacy-minimized while retaining capability identity, version, provider,
foreground, invocation, cursor, terminal state, and postcondition result.
