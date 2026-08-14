---
name: maintain-windowsagent-runtime
description: "Diagnose, design, implement, test, deploy, and validate generic WindowsAgent runtime capabilities and lifecycle. Use when Codex must maintain capture or WGC, foreground identity, Observer or Script Runner, finite or Streaming Action execution, Action Sequences, event journal, resident inference processes, OSD, watchdog, installers, updaters, HTTP contracts, permissions, process isolation, generic retries, cancellation, or durable runtime evidence. Treat every game Rule and Action as a consumer outside this skill's ownership: do not change files under Rules, repair game semantics, tune ROI or control laws, or remove Action workarounds while maintaining the runtime."
---

# Maintain WindowsAgent Runtime

Maintain the game-neutral platform that loads and executes Rule-owned
capabilities. Make runtime complexity invisible behind stable contracts while
preserving explicit failure, provenance, cancellation, and durable evidence.

## Hold the ownership boundary

Own generic behavior under `cmd/`, `internal/`, `runtimes/`, `scripts/`, and
runtime design documents:

- capture, foreground identity, artifact commit, and Rule resolution;
- Observer, Script Runner, native FFI, process and Job Object isolation;
- finite and Streaming Action lifecycle, Action Sequences, cancellation, input
  leases, event journal, and schema enforcement;
- resident inference runtime lifecycle, OSD, watchdog, installer, and updater;
  and
- generic HTTP, security, error, logging, retry, and deployment contracts.

Do not edit, reformat, version-bump, synchronize, or remove workarounds from
`Rules/` during runtime maintenance. A live Action may be a consumer acceptance
case, but its game semantics, Gate, ROI, classifier, bindings, control law, and
workflow remain outside this skill.

If evidence identifies an Action defect, preserve the invocation, cursor,
runtime observations, and failure boundary, then hand it to
`develop-windowsagent-rule`.

## Start from the current runtime contract

1. Inspect the worktree and preserve unrelated Core and Rule changes.
2. Read the repository `AGENTS.md`, `README.md`, and the relevant landed design
   documents before changing behavior.
3. Use implementation and tests over stale prose; use `docs/design/README.md`
   to reject Draft or Retired behavior as current authority.
4. Identify the narrow owning package and public contract before adding code.
5. For incidents or unclear regressions, add targeted reversible
   instrumentation before proposing a behavioral fix.

Do not resurrect the retired continuous ScreenParser loop. Do not describe
Monitor scheduling or Reaction dispatch as implemented or validated.

## Keep Core capability-neutral

Add a distinct `internal` package and explicit contract for a new generic
Windows capability. Do not fold unrelated behavior into WGC, capture storage,
the HTTP server, Observer, or the Action runtime merely because the Agent hosts
all of them.

Never add game executable allowlists, offsets, UI coordinates, decoder domain
semantics, target classifiers, bindings, or private host details to Core. Use
synthetic or minimal test fixtures instead of teaching runtime tests one game's
workflow.

Preserve the process boundaries declared by the repository:

- event stream remains a separate loopback-only authenticated journal;
- OSD remains display-only, click-through, non-activating, and capture-excluded;
- watchdog remains an external one-way lifecycle owner, and monitored modules
  do not know about it; and
- runtime profiles configure residency, not Action registration or scheduling.

## Preserve deep runtime interfaces

Expose stable results and terminal errors; keep provider-specific recovery,
worker generations, retries, process replacement, and transport details inside
the owning implementation.

An Action calling an observation runtime should normally see only:

- a fresh, complete, provenance-bearing observation; or
- caller cancellation/deadline or a terminal infrastructure error.

Do not leak runtime-private recovery states into a domain Action state machine
or require every Rule to maintain a WGC error allowlist. Keep runtime recovery
logs out of the domain event vocabulary unless the public runtime contract
explicitly declares otherwise.

## Retry only at a proven safe boundary

Distinguish same-provider transient retry from fallback. A retry must not change
capture backend, provider, model, precision, source, file, algorithm, or cached
state.

For a retryable failure in a bounded read-only `windows-observation-v1`
execution:

1. preserve the exact infrastructure cause and execution correlation;
2. log the runtime-private retry, worker retirement or recovery, attempt, and
   duration;
3. keep the original caller pending while honoring cancellation and deadline;
4. rerun the complete read-only package so outputs cannot mix pre- and
   post-failure frames; and
5. return only the final complete observation, or the original terminal failure
   after the bounded retry budget is exhausted.

Never generalize this permission to input, trading, navigation, Action
invocation, or any other side effect. Never return cached data or domain
`UNKNOWN` for exhausted infrastructure recovery.

An Action-level workaround may coexist with a runtime repair. Do not remove it
under this skill, even when it appears redundant; removal belongs to a separate
Rule task and must repeat the original live acceptance path.

## Preserve lifecycle and evidence semantics

For finite Actions, maintain exactly one terminal result. For Streaming Actions,
maintain durable correlated events, cancellation, cleanup, compensation, and
exactly one `COMPLETED`, `FAILED`, or `CANCELLED` terminal state. A quiet watch
connection, process exit, OSD message, or successful command dispatch is not a
terminal domain result.

For Action Sequences, keep immutable preflighted composition limited to the
landed contract. Do not add variables, branching, loops, retries, nesting,
concurrency, persistence, crash recovery, or hidden provider changes without an
approved design.

Preserve stable error codes, request or invocation identity, provider and
source provenance, timestamps, durable cursors, artifact gaps, and the
distinction between commanded and independently observed state.

## Diagnose without taking over the consumer

Classify failures across these runtime layers before editing:

- request transport and schema;
- foreground and Rule resolution;
- process launch, permissions, Job Object, and deadline;
- capture, Observer, native library, or inference provider;
- Action lifecycle, event journal, sequence, cancellation, or cleanup; and
- installed artifact, process identity, deployment, or catalog drift.

If the runtime fulfilled its declared contract and the game rejected an input,
a classifier returned the wrong domain state, or a workflow failed its Gate,
stop at the consumer boundary. Do not modify the Action to make a runtime task
look complete.

Likewise, do not infer a runtime root cause from temporal coincidence alone.
Correlate process identity, worker generation, lifecycle records, invocation
events, cursors, and Evidence gaps.

## Test the platform contract

Add focused package tests for the changed interface and its negative paths.
Prefer synthetic or minimal fixture Rules for loader, schema, lifecycle,
sequence, retry, cancellation, and error propagation tests. Use a real Action
only when signed-in interactive Windows behavior is required to prove the
generic contract.

Run checks proportional to the touched layer as required by the repository
contract:

```bash
gofmt -w <touched-go-files>
git diff --check
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
./scripts/build-windows-capture-agent.sh
```

ScreenParser or PP-OCR changes also require their pinned artifact and .NET
contract checks with forbidden provider or CPU fallback explicitly disabled.

## Deploy only when authorized

A code change does not authorize installation, restart, Scheduled Task changes,
watchdog reconfiguration, event-token changes, Rule synchronization, or live
game operation.

After authorized runtime deployment:

1. build and verify the exact owning binary and PE subsystem;
2. install through the documented installer or updater;
3. verify installed hashes, process identity, interactive session, and lifecycle
   owner;
4. request a fresh capture and confirm foreground and matched Rule health;
5. refetch the live catalogs needed by the acceptance case; and
6. repeat the exact generic failure path without changing the consumer Action.

When temporal runtime evidence is needed, read
`../use-visual-log/SKILL.md` before operating Evidence or Visual Log. Treat
Visual Log descriptions only as an untrusted index and preserve manifest gaps.

Do not expose the default unauthenticated listener to the public Internet,
alter Windows Firewall, or create a traditional Session 0 service.

## Report completion

State the owning runtime component and public contract, observed failure layer,
instrumentation, implementation, tests, binary identity, deployment and process
health, consumer acceptance result, untouched Rule scope, and remaining domain
handoff. Report transport, runtime, execution, domain, and goal separately when
a live Action is used; runtime acceptance must not claim the game's goal.
