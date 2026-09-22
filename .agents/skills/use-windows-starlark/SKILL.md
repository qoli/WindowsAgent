---
name: use-windows-starlark
description: "Experimental authoring, preflight, invocation, and evaluation of bounded caller-authored windows-starlark-action-v1 packages for general Windows process and filesystem workflows. Use for deterministic multi-step automation; excludes persistent Rules, game Actions, and repository development."
---

# Use Windows Starlark — experimental

This Skill is a provisional product hypothesis. Long-term authoring stability
and compatibility remain under evaluation. One successful workflow does not
establish a stable contract. Prefer repeated real Harness use and preserve
failures and awkward cases before expanding the guidance.

This is an optional companion to ordinary `use-windows-pc` onboarding. It is
not included in the stable end-user Skill bundle and must not be promoted there
without an explicit decision backed by practical testing. No WindowsAgent
repository developer Skill is required.

## Choose the capability

Use Starlark when one bounded deterministic workflow needs ordered Windows
process/filesystem operations, conditional decisions, and a schema-checked
result. Use the ordinary PC Skill for a single capture, SFTP operation,
managed process, PowerShell file, or literal key press. Use delegated Pi when
the worker must plan and iterate rather than execute a known procedure.

This runtime currently has no screen, key, pointer, OCR, Observer, or native
FFI host API. Do not invent those APIs or bypass missing support with an input
provider inside a child process. Existing installed Rule Actions retain their
domain ownership. Persistent Rule plugins, game-specific Actions, observation
packages, streaming Rule Actions, and Rule deployment/synchronization belong
to a later product phase and repository development guidance; this Skill does
not teach or require that workflow.

## Prepare and author

1. Define the authorized effect, required Windows files/programs, expected
   result, and cleanup in `TASK.md`. Target the user's configured PC; use the
   installed `use-windows-pc` resolver for its private configuration and normal
   endpoint. Do not infer a host from examples or prior sessions. Configuration
   is not authorization for unrelated mutations.
2. Read [references/authoring.md](references/authoring.md) before writing the
   package. It contains the current manifest, dialect, and complete host API.
   Copy [assets/read-file](assets/read-file) to a private working directory as
   a five-file starting point. Replace its read-only task with the requested
   workflow and update both schemas together with the code.
3. Keep `inputs.json`, outputs, and experiment notes **outside** the package
   directory. Keep actual Windows paths and private values in caller inputs,
   not the reusable Skill. Pick manifest limits from the workload, not from a
   supposed universal default.
4. Read [references/invocation.md](references/invocation.md) for the current
   local client prerequisite, preflight, submission, watch, and stop procedure.
   If the experimental client or runtime is unavailable, report that concrete
   dependency; ordinary PC onboarding can still proceed. Do not substitute a
   different host, transport, privilege, or runtime.

## Evaluate and finish

Local preflight checks structure and syntax; it never executes the workflow.
Only WindowsAgent performs real execution, using its installed user token,
environment, and interactive session. The package cannot select another
session or elevate itself. HTTP acceptance is not completion.

Follow the invocation to its durable terminal result. Check returned output
against the requested postcondition, including fresh independent desktop or
application evidence when that is the goal. Report cancellation or failure
with the original error code, stage, invocation ID, and last durable cursor.
Do not automatically resubmit after an ambiguous network failure: side effects
may already have happened.

There is no automatic rollback or guaranteed finally-style cleanup. On success,
failure, or cancellation, inspect the task-owned residual files/processes and
perform only cleanup authorized by the task. Preserve diagnostic evidence
before deleting temporary artifacts; report unresolved residue explicitly.

Keep a private experiment note containing the Skill/runtime/client revision
when known, package digest/version, task and expected postcondition, preflight
result, invocation ID/cursor, terminal output or typed failure, observed effect,
and cleanup status. Record awkward authoring steps and unsupported needs,
including unsuccessful attempts. Redact secrets and private host/file data
before sharing a minimal reproduction. Do not publish logs or update an issue
unless requested. Refine the Skill from repeated use, without inferring new
compatibility promises from this experiment.
