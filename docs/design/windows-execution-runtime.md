# Windows Execution Runtime

## Status

**Partially landed as `windows-exec-v1`.** Structured process run, detached
start, Windows PowerShell file execution, the durable HTTP lifecycle, and the
console client are implemented. Signed-in Windows process-tree, desktop,
PowerShell, and detached-start acceptance remain required before this design
can be classified as Landed.

## Responsibility

`windows-exec-v1` is the direct convenience interface for one common Windows
process operation. It removes the Starlark package ceremony from operations
that do not need orchestration:

| Operation | Terminal meaning |
| --- | --- |
| `run` | The owned process tree exited and stdout/stderr were collected. |
| `start` | Windows created one invocation-detached process and recorded its identity. |
| `powershell-file` | The owned Windows PowerShell process running one remote `.ps1` exited. |

Starlark remains the caller-authored language for multi-step orchestration,
conditions, filesystem logic, activities, and postconditions. The execution
runtime never generates a Starlark package internally.

## Request interface

`POST /v1/executions/invoke` accepts one strict `schemaVersion: 1` JSON object.
Every operation uses an argument array; there is no command-string field or
shell grammar.

Common optional fields are `argv`, absolute `cwd`, `env`, base64-encoded
`stdin`, `window`, `maxOutputBytes`, and `timeoutMilliseconds`. `window` is
`normal` or `hidden`; omission means hidden for `run` and `powershell-file`, and
normal for `start`. Zero `maxOutputBytes` means that the caller did not request
an override and resolves to the 65,536-byte per-stream inline-output limit. A
positive value may lower that limit but cannot exceed 65,536 bytes. The first
stdout or stderr byte beyond the effective limit terminates the owned Job and
returns `EXEC_OUTPUT_LIMIT_EXCEEDED`; output is never silently truncated.
Zero `timeoutMilliseconds` means that the caller did not request an execution
deadline; the durable stop operation remains available.

`run` and `start` require `executable` and reject `scriptPath`.
`powershell-file` requires an absolute `.ps1` `scriptPath` and rejects an
executable override. `start` rejects stdin, output limits, and execution
timeouts because it does not own the process after creation.

## Process semantics

`run` creates the process suspended, assigns it to a new Windows Job Object
with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, and only then resumes it. It waits
for the direct process, then closes the Job so that any surviving descendants
cannot outlive the invocation. Stop, timeout, or Agent shutdown terminates the
Job rather than killing only the direct child.

`start` deliberately does not enter the invocation Job and its invocation is
not interruptible. It returns PID, process creation time, resolved executable,
Agent session, and start time after successful creation. The process inherits
the Agent's token, session, desktop, and any outer lifecycle Job owned by the
installed Scheduled Task. It can outlive invocation completion, but Agent or
Task shutdown may still terminate it. A completed `start` result proves only
process creation. It does not prove that the process remained alive, displayed
a window, loaded an application state, or achieved a domain goal. A process
that requires managed residency belongs in an owned Scheduled Task and
Watchdog target instead.

Output is byte-safe. Every stdout and stderr value reports base64 bytes and byte
length; valid UTF-8 additionally exposes `text`. Non-UTF-8 output is not
discarded, guessed, or decoded using an implicit code page. This inline result
is not a bulk-log data plane. A caller that needs larger output must write it to
an explicit Windows file, return only bounded metadata such as path, byte count,
and digest, and retrieve the file separately through the documented SFTP data
plane.

## PowerShell file adapter

`powershell-file` invokes the exact built-in Windows PowerShell 5.1 executable
under `%SystemRoot%` with:

```text
-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File <scriptPath> <argv...>
```

The `.ps1` already exists on Windows, normally after transfer through the
separate SFTP data plane. Script text is never inserted into `-Command` or
`-EncodedCommand`. The runtime does not select `pwsh.exe`, `cmd.exe`, SSH,
Windows MCP, or another adapter when the declared executable is unavailable.

## Durable lifecycle and client

The invoke route returns HTTP `202` with the common Action invocation ID,
status, watch, and stop targets. Start and terminal events retain the runtime,
operation, request digest, typed error code/stage, and exact output result.
Agent restart terminalizes an unfinished execution rather than resuming it
against unknown process state.

`windows-exec` is a local console client with explicit `run`, `start`, and
`ps1` subcommands. It submits structured arguments, follows the returned status
target, prints the terminal JSON, and returns nonzero for transport, failed, or
cancelled invocations. Its `ps1` subcommand takes a remote Windows path; SFTP
transfer and execution remain two visible operations in this version.

## Execution context and failure

All processes are created by the GUI-subsystem `windows-capture-agent.exe` and
inherit its installed Windows token, environment, session, and interactive
desktop. A request cannot select another token, session, account, or elevation
level. Foreground-window identity is recorded when available but is not a
precondition for this Rule-independent host capability.

Unknown operations or fields, conflicting operation fields, invalid paths or
environment entries, process lookup/creation, Job assignment, resume, wait,
timeout, output-limit, and PowerShell discovery failures are terminal. There is
no fallback to Starlark, an alternate shell, another PowerShell edition, SSH,
MCP, or a detached launch when owned execution fails.

Before a terminal result is sent to the event journal, the complete append
request and a worst-case committed event envelope are size-checked against the
journal's 1 MiB record contract. A result that cannot be represented becomes a
small durable `action.failed` event with `EXEC_RESULT_TOO_LARGE`; it is never
reported as completed and never sent as an oversized HTTP request.

## Remaining acceptance

Live Windows acceptance must verify the installed Agent hash/run level/session;
argv and environment preservation; byte-safe stdout/stderr and nonzero exit
data; cancellation and timeout of a descendant tree; visible and hidden window
selection; PowerShell arguments and script exit behavior; detached start
identity and survival across invocation completion while retaining the Agent's
outer lifecycle ownership;
durable replay; and coexistence with capture, Starlark, and SFTP.
