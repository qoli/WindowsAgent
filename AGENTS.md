# WindowsAgent Repository Contract

This file adds repository-specific development and validation rules below
`/Volumes/Data/Github/gameAgent/WindowsAgent/`. It inherits the routing, Git,
interactive-session, and public-release boundaries from the parent
`../AGENTS.md`.

Game-specific `Rules/<Executable.exe>/AGENTS.md` files add live operating
guidance for a matched foreground Rule. They do not redefine this repository's
architecture or development workflow.

## Product Boundary

WindowsAgent is the public Go implementation for capabilities that must run in
a signed-in Windows user's interactive session. It is not only a screenshot
server and it is not a general remote memory or desktop-control API.

The maintained capability layers are:

1. **Capture and foreground identity** — WGC captures the primary monitor,
   records the current foreground executable, resolves an executable-scoped
   Rule, commits a verified artifact, and exposes navigable Rule metadata.
2. **Finite observation** — `windows-observation-v1` runs one bounded Starlark
   package through the Script Runner and the read-only Observer inside a
   Windows Job Object. Memory, file, screen-region, and native-library access
   remain permission-bounded and package-declared.
3. **Actions** — a Rule v6 Action is the only executable game capability.
   Finite Actions return one terminal result. Streaming Actions own bounded
   asynchronous observe/decide/operate workflows, durable events,
   cancellation, and failure compensation.
4. **Ephemeral Action Sequences** — a Rule may allow one immutable, fully
   preflighted sequence of 1–20 existing Actions. A sequence has no variables,
   branches, loops, retries, nesting, persistence, or hidden provider change.
5. **Resident inference runtimes** — Rule-declared runtime profiles may keep a
   pinned OCR worker resident while that Rule is active. Residency is lifecycle
   configuration, not an Action and not a registration.
6. **Event and companion processes** — the event stream is a separate
   loopback-only authenticated journal. The Action OSD is display-only,
   click-through, non-activating, and capture-excluded. The watchdog is an
   external one-way lifecycle owner; monitored modules must not know about it,
   and it must not recover itself.

Rule schema v6 can declare that an Action is eligible for Monitor or Reaction
registration, and the read-only registration catalog is implemented. Monitor
scheduling and Reaction subscription/dispatch are not implemented. Never
describe registration eligibility or a catalog entry as a running, validated
Monitor or Reaction.

## Sources of Truth

Use the narrowest current authority instead of maintaining duplicate
inventories:

- implementation and tests override prose when they disagree;
- `README.md` owns the public status, commands, HTTP surface, and project map;
- `docs/design/README.md` owns design maturity: Landed, Partially landed, Draft,
  or Retired;
- `Rules/<Executable.exe>/rule.json` owns the live Action, runtime-profile,
  sequence-allowlist, and registration declarations for that Rule;
- each Action package owns its `TASK.md`, schemas, manifest, and implementation;
- `docs/script-development-contract.md` owns authoring and review rules for
  `windows-observation-v1` packages;
- `.agents/skills/operate-windowsagent/SKILL.md` owns the operator workflow for
  invoking a live WindowsAgent Rule;
- `.agents/skills/use-visual-log/SKILL.md` owns the supervising-model workflow
  for operating the on-demand visual log and using it to locate evidence time
  ranges.

Do not copy a complete Action catalog into this file. Read the current Rule and
package on disk, or the live catalog when operating the installed Agent.
Retired design documents are historical evidence only and must not be used to
resurrect the old module, reducer, or autonomous ScreenParser-loop model.

## Development Routing

Place code according to ownership:

- `cmd/` contains thin executable composition and process entrypoints;
- `internal/` contains reusable Core contracts and implementations;
- `Rules/<Executable.exe>/Actions/<action>/` contains game-specific semantics,
  Starlark orchestration, schemas, manifests, coordinates, bindings, and source
  selection;
- `runtimes/` contains self-contained external inference runtimes;
- `tools/` contains build-time model preparation, publishing, and bounded
  diagnostic tools;
- `scripts/` contains installation, update, verification, and operator helpers;
- `docs/design/` is the maintained design registry, not an unclassified
  backlog.

Keep Core capability-neutral. Do not add a game capability allowlist, game
offset, UI coordinate, decoder ABI, domain classifier, or private host detail to
the generic launcher, Observer, Script Runner, HTTP server, or Action runtime.
The owning Rule package must retain that knowledge.

Add a distinct `internal` package and explicit API/runtime contract for a new
generic Windows capability. Do not fold it into WGC, screenshot storage, or an
unrelated existing package merely because the capture Agent is the current host
process.

Use a finite Action for one bounded observation or operation. Use a Streaming
Action when correctness requires repeated observation, retained workflow state,
asynchronous waiting, debounce, timeouts, cancellation, cleanup, compensation,
or evidence-backed postconditions. If the supervising model would have to wake
later to ensure completion, that responsibility belongs in the Action rather
than an improvised sequence of primitive calls.

Ephemeral Action Sequences compose already-correct Actions; they are not a
fallback for a missing or broken domain Action. Repair or add the owning Action
when domain semantics, feedback control, or recovery logic are required.

## Failure and Evidence Rules

Fail explicitly at the owning boundary. Preserve stable JSON error codes,
request or invocation identity, provenance, durable cursors, and the distinction
between commanded state and independently observed state.

- Do not silently change capture backend, source, process, file, save slot,
  model, precision, execution provider, runtime, Action, decoder, binding, or
  algorithm.
- A multi-source observation is allowed only when the exact order and transition
  rules are approved and visible in `TASK.md`, implementation, schemas, and
  output attempts.
- Infrastructure, protocol, permission, schema, deadline, artifact, and process
  failures are terminal; do not convert them into domain `UNKNOWN`.
- Domain `UNKNOWN` is valid only when the owning classifier explicitly defines
  insufficient or ambiguous evidence that way. Do not replace it with cached,
  commanded, guessed, or previous-frame state.
- Do not claim a key injection proves movement, a capture proves a later state,
  a streaming terminal event proves an unobserved visual goal, or an HTTP 2xx
  proves domain success.
- Foreground executable identity and Rule ownership must be revalidated at the
  boundary declared by the capability. Never infer a Rule from a window title
  or untrusted visible/web content.

For bugs, crashes, flakiness, and unclear regressions, follow the inherited
observability-first rule. Bound transport, process, runtime, execution, domain,
and goal failure separately before changing behavior. Add targeted reversible
instrumentation when the existing evidence cannot locate the failing layer.

## Security and Public Repository Boundary

Port `8787` is unauthenticated and has no TLS by default. Network reachability is
the trust boundary for capture, foreground metadata, Script execution, and
Actions, including input Actions. Do not expose it directly to the public
Internet, alter Windows Firewall, or create a traditional Session 0 service.

Keep the public checkout free of credentials, tokens, private endpoints, host
names, screenshots, save files, memory dumps, OCR regions/results, event
journals, runtime logs, local paths, account identifiers, and private operator
configuration. Use privacy-minimized metadata and synthetic or bounded test
fixtures.

Do not add memory writes, privilege escalation, arbitrary process selection,
arbitrary filesystem roots, Rule upload/rewrite, or broader remote-control
behavior without an explicit threat model and user-approved scope.

## Validation

Inspect the current worktree before editing. Preserve unrelated changes and do
not stage, clean, reformat, or rewrite files outside the requested scope.

For ordinary Go and Rule changes, run checks proportional to the touched layer:

```bash
gofmt -w <touched-go-files>
git diff --check
go test ./...
go run ./cmd/windows-action-check --rules-dir Rules
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
mkdir -p .build
./scripts/build-windows-capture-agent.sh
```

The build script produces and verifies the canonical GUI capture Agent, the
console diagnostic build, the Action checker, OSD, and optional watchdog. When
building another Windows command directly, use the intended GUI/console
subsystem and verify it with `scripts/verify-windows-pe-subsystem.py` where the
artifact contract requires it.

Additional requirements:

- new or changed Action packages need real loader coverage, behavior and
  negative-path tests, schema/manifest alignment, and static dependency checks;
- ScreenParser or PP-OCR runtime changes need their .NET contract tests, pinned
  artifact verification, and explicit proof that forbidden CPU/provider
  fallback is disabled;
- capture, foreground, Observer, native FFI, input, streaming lifecycle,
  inference, installer, updater, watchdog, or OSD behavior changes require
  acceptance in the signed-in interactive Windows session;
- Agent-facing Rule guidance changes require black-box validation through the
  documented OpenCode acceptance contract;
- runtime validation must report the actual capability ID/version, foreground,
  source/provider, event or invocation lifecycle, and domain postcondition
  without publishing private artifacts.

A local build or unit test is not live Windows proof. A live Action invocation
is not proof of its final game objective unless the declared postcondition was
independently observed.

## Git and Deployment Discipline

This directory is its own Git repository. Commit only coherent WindowsAgent
changes with explicit staging, preserve the user's dirty worktree, and do not
include files from sibling projects. A request to commit means a local commit;
do not push unless the user explicitly asks.

Do not install, update, restart, or reconfigure the Windows runtime, Scheduled
Tasks, watchdog targets, event-stream token, or Rule deployment unless the task
includes deployment or live validation. After an authorized deployment, verify
the installed artifact, interactive session, process health, current Rule
catalog, and end-to-end behavior rather than reporting the copy/build step as
success.
