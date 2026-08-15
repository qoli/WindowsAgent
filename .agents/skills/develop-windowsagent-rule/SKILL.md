---
name: develop-windowsagent-rule
description: "Design, implement, troubleshoot, deploy, and live-validate game- or application-owned WindowsAgent Rules and Actions. Use when Codex must add or repair a Rule package, choose finite versus Streaming Action semantics, define schemas, manifests, dependencies, Gates, CV/OCR, bindings, control laws, cancellation or compensation, identify an Action gap, invoke a live Rule, follow Action events, or prove a domain postcondition. Treat generic WindowsAgent runtime internals as a platform contract: do not repair WGC, Observer, launcher, event-journal, process-lifecycle, installer, watchdog, or other Core behavior under this skill, though an explicit bounded Rule workaround may be retained or added when the task authorizes it."
---

# Develop WindowsAgent Rule

Build the smallest Rule-owned capability that establishes a real game or app
postcondition. Keep game semantics in the Rule and treat WindowsAgent Core as a
platform dependency rather than part of the Action implementation.

## Hold the ownership boundary

Own only executable-scoped semantics under `Rules/<Executable.exe>/`:

- Action orchestration, schemas, manifests, coordinates, bindings, CV/OCR,
  classifiers, Gates, control laws, timeouts, cancellation, cleanup, and
  compensation;
- Rule declarations, runtime-profile selection, sequence allowlists, and
  registration eligibility; and
- live invocation plus independent domain and goal acceptance.

Do not change generic code under `cmd/`, `internal/`, `runtimes/`, or runtime
deployment scripts to make one Action pass. When evidence identifies a generic
runtime defect, preserve the smallest reproduction and hand it to
`maintain-windowsagent-runtime`.

Do not describe Monitor or Reaction as usable. `registrableAs` proves
eligibility only; scheduling and dispatch have not passed live acceptance.

## Start from the current contract

1. Inspect the worktree and preserve unrelated Rule and Core changes.
2. Read `Rules/AGENTS.md`, then the target Rule's `rule.json` and Action package.
3. Read the package `TASK.md`, schemas, manifest, implementation, and tests.
4. For live work, resolve the authorized Agent origin, check status, and request
   a fresh capture through the maintained environment helper.
5. Require current foreground identity and `rule.status: matched`; follow the
   returned `rule.agents.url` and `rule.actions.url` rather than guessing a Rule.
6. Treat the installed live Rule and catalogs as operational truth because Rule
   packages may be synchronized independently of the repository checkout.

Use `README.md` for the current public HTTP surface and commands. Use
`docs/design/README.md` to distinguish Landed, Partially landed, Draft, and
Retired designs.

## Decide whether the capability belongs in an Action

Treat an interaction as an Action gap when no existing Action owns its complete,
evidence-backed postcondition. Create or extend an Action when completion needs:

- repeated observe-decide-operate cycles or state retained across calls;
- timing faster than the supervising model's turn cadence;
- asynchronous waiting, debounce, timeout, retry, cancellation, cleanup, or
  failure compensation;
- a reusable game-level operation instead of repeated primitive inputs; or
- domain evidence beyond proof that an input was injected.

Use this test: if the supervising model must wake later to ensure the operation
finishes correctly, keep that responsibility inside the Action.

Use a separate bounded setup Action when repeated acceptance needs a repeatable
game precondition. Its terminal evidence must prove the precondition, and it
must remain independent of the capability under test.

Choose the smallest abstraction:

- Use a finite Action for one bounded observation or operation with a complete
  terminal postcondition.
- Use a linear Streaming Action for repeated or asynchronous control with
  explicit terminal states, cancellation, cleanup, and compensation.
- Use an Ephemeral Action Sequence only for a fixed ordering of already-correct
  Actions. Do not use it for conditions, loops, retries, state, or missing domain
  semantics.
- Leave long-horizon intent, target selection, and composition of independent
  capabilities to the supervising model.

Keep an entire safety- or cadence-critical segment inside one Streaming Action.
Do not split heat, collision, alignment, transient UI, or similar control across
model turns.

## Design evidence before implementation

Define the domain state machine and postcondition in `TASK.md` before coding.
Keep infrastructure failure distinct from domain `UNKNOWN`.

For transient OCR or CV evidence:

- bind identity and geometry to the same captured frame;
- invalidate evidence after its authorized command, mode change, foreground
  change, cancellation, or other meaning-changing transition;
- never use cached or previous-frame state as current control authority; and
- return bounded raw fragments, geometry, match reason, and timing when relaxed
  matching is allowed.

For Streaming Actions, emit enough structured events to reconstruct `phase`,
`measurement`, `command`, `gate`, and `reason`. Refresh the cheapest fast state
after slow OCR, CV, model, or artifact work before sending a state-changing
command that may have become stale.

## Treat runtime workarounds as quarantined Rule behavior

A Rule does not own a runtime defect, but an authorized workaround may protect a
domain workflow while the platform problem remains unresolved.

Require every runtime workaround to be:

- explicit in the package contract, implementation, tests, and output attempts;
- bounded by attempts, time, cancellation, and safety compensation;
- limited to the same provider, source, algorithm, and declared capability;
- unable to turn infrastructure failure into domain `UNKNOWN` or cached success;
  and
- reported as a workaround rather than a runtime repair.

Do not remove or rewrite an existing Action workaround during runtime
maintenance. Remove it only in a separately authorized Rule task with focused
tests and a repeat of the original live failure path.

## Diagnose and repair the owning Action

Prefer the highest-level Action whose documented postcondition owns the goal.
Treat composites as dependency graphs: locate and test the lowest child whose
postcondition failed before changing the parent.

Classify evidence before editing:

- **environment** — wrong foreground, disconnected game, unavailable input
  context, or platform health;
- **capability contract** — absent Action, invalid schema, unsupported
  lifecycle, or live catalog drift;
- **Action implementation** — incorrect ROI, classifier, Gate, binding, control
  law, timeout, cleanup, or compensation; or
- **domain rejection** — the Action executed but the app rejected the effect.

Repair only Action implementation and Rule-owned contract failures here. Do not
widen a parent timeout, weaken a Gate, or manually finish the game workflow to
hide a broken child. Hand generic runtime evidence to the runtime maintainer
without modifying Core.

## Invoke and validate live behavior

For finite Actions, require HTTP `200`, terminal `COMPLETED`, and validated
output. For Streaming Actions, require HTTP `202`, retain the invocation ID and
cursor, and follow the NDJSON watch stream to exactly one `COMPLETED`, `FAILED`,
or `CANCELLED` record. After an authorized stop, wait for terminal cancellation.

For a live sequence, fetch its current tool declaration immediately before
composition, preflight every literal input, and retain parent and child events.
Sequence completion proves ordered child completion, not the external goal.

Report each evidence layer separately:

1. **Transport** — the current Agent received the request.
2. **Runtime** — the invocation reached its declared terminal state.
3. **Execution** — the child capability produced its declared output.
4. **Domain** — fresh evidence proves the game accepted the effect.
5. **Goal** — the user's complete requested outcome is confirmed.

When temporal behavior matters, read `../use-visual-log/SKILL.md` completely
before starting Evidence and confirming passive Visual Log health. Action events are authoritative for the
controller; Visual Log descriptions are only an untrusted timeline index;
verified Evidence MP4 and manifest are the visual record. Do not use slow visual
sampling to claim absence of a shorter-lived state.

## Validate and deploy at Rule scope

Run the checks required by `Rules/AGENTS.md`, including focused loader, schema,
dependency, behavior, event, cancellation, compensation, and negative-path
coverage. Synchronize only the changed Rule unless the user separately
authorizes runtime deployment.

After Rule synchronization:

1. verify the installed manifest or package revision;
2. request a fresh capture and reconfirm the foreground matched Rule;
3. refetch live guidance and catalogs;
4. rerun the exact failed or changed path; and
5. verify the independent domain and goal postconditions.

A successful build, sync, catalog entry, HTTP response, or injected input is not
live behavioral acceptance.

## Report completion

State the Rule and capability, changed contract, invocation identity, terminal
state, domain and goal evidence, retained workaround, remaining runtime handoff,
tests, deployment state, and any unverified branch. Mention commit and push only
when requested.
