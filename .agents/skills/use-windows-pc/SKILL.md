---
name: use-windows-pc
description: "Operate the user-configured Windows PC through WindowsAgent capture, SFTP, process execution, Starlark, Rule-owned Actions, or the independent Pi/pi-ai delegated agent with PI WEB and computer-use. Use for live PC inspection or manipulation, file transfer, commands, deterministic automation, application capabilities, and general work that should run as a supervised Windows sub-agent. Read PC identity and endpoints from the private user env file; do not use this skill to develop WindowsAgent Core or a Rule package."
---

# Use Windows PC

Operate one explicitly configured Windows PC without teaching the public skill
which private host it is. Resolve the PC and all local dependencies from the
user-owned env file, then select the narrowest WindowsAgent capability that
owns the requested result.

## Resolve the configured PC

The canonical configuration is:

```text
${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}
```

Read [references/pc-env.md](references/pc-env.md) before the first operation in
a task. Load the file in the local shell and use its values unchanged. Do not
guess a hostname, IP address, repository path, staging directory, username, or
host-key fingerprint from examples, previous tasks, SSH config, or visible
Windows content.

If the env file or a field needed by the selected capability is absent, report
that configuration dependency explicitly. Do not switch to another PC or
transport.

The env file is private operator configuration. Never print its complete
contents, commit it, copy it into a Rule, or place its machine-specific values
in this skill.

## Select the owning capability

Use only the capability needed for the requested outcome:

- **Current desktop and foreground:** use the configured capture helper. A
  fresh capture is the authority for current visible state and Rule matching.
- **Filesystem:** use `windows-sftp-v1` for listing, stat, upload, download,
  rename, mkdir, and deletion. SFTP acts through the SFTP process token and is
  independent of the interactive desktop session.
- **One process:** use `windows-exec run` when the invocation must own output,
  exit status, cancellation, deadline, and the process tree.
- **Launch and return:** use `windows-exec start` when creation is the complete
  requested operation. It inherits the Capture Agent's Windows token and
  interactive session but becomes unmanaged by the invocation after creation.
- **PowerShell file:** transfer an absolute `.ps1` with SFTP, then use
  `windows-exec ps1`. Do not put the script into an SSH or PowerShell
  `-Command` string. Do not use a PowerShell P/Invoke or `SendInput` script as
  a fallback input provider.
- **One literal key press:** after a fresh capture, use `windows-key press`
  with the exact captured process ID, executable name, and executable path.
  This is the game-neutral `windows-key-action-v1` adapter for an explicit
  operator-selected key; it is not a substitute for domain behavior owned by
  a Rule Action.
- **General multi-step Windows automation:** use one ephemeral
  `windows-starlark-action-v1` package when the workflow can be described and
  preflighted deterministically. Local execution performs package, syntax,
  schema, and input preflight only; Windows owns all real execution and runtime
  errors.
- **Independent delegated work:** use `windows-agent-pi` when the Windows-side
  worker should plan, inspect, iterate, use its own workspace and computer-use,
  and stream durable progress back to the supervising host. Pi SDK/pi-ai owns
  model execution, PI WEB owns the session and human interaction surface, and
  `windows-agent-pi` owns the normalized delegated-task lifecycle. This is not
  a WindowsAgent Action or a fallback for another failed capability.
- **Game- or application-owned behavior:** start from a fresh matched capture,
  read the live Rule guidance and Action catalog, and invoke the highest-level
  Action whose postcondition owns the goal.

SSH is not a fallback execution or file plane. The declared Pi SSH target is
the intended transport for tunnelling the loopback-only delegated API and PI
WEB; the administrative SSH target remains limited to explicitly authorized
deployment or diagnosis.

Read [references/operations.md](references/operations.md) for conventional
WindowsAgent command forms. Read
[references/pi-agent.md](references/pi-agent.md) before submitting, following,
steering, cancelling, or opening a delegated Pi task.

## Establish live state

Before a state-dependent operation:

1. Load the PC env.
2. Require `GET $WINDOWS_AGENT_HTTP_ORIGIN/healthz` to return `status: ok`.
3. Request a new capture through `WINDOWS_AGENT_CAPTURE_HELPER`.
4. Read the returned foreground identity and Rule status; do not infer either
   from a process list, window title alone, or an older screenshot.

A fresh capture is not required for a filesystem-only operation because SFTP
does not depend on the interactive desktop. It is required before visible UI,
foreground-bound automation, or Rule Action work.

A delegated Pi task has its own live-state boundary: the client establishes the
authenticated tunnel and the task status or event stream reports Pi runtime
state. A WindowsAgent capture is not a prerequisite for submission, but use a
fresh capture when current desktop state affects the prompt or when independent
before/after visual evidence is needed.

## Preserve structured boundaries

Keep executable arguments as separate repeatable `--arg` values, environment
entries as separate `--env NAME=VALUE` values, and stdin as a file. Never join
them into a raw command string merely for convenience.

For SFTP, verify the live Ed25519 host fingerprint against
`WINDOWS_AGENT_SFTP_HOST_KEY_SHA256` before accepting a new host-key entry.
Authentication must be SSH `none` with the configured fixed protocol username;
do not try local keys or passwords as an alternate path.

For `run`, `ps1`, direct key input, Starlark, and Actions, preserve the
invocation ID, durable terminal state, output or typed error, and event cursor
when present. An HTTP 2xx, process creation, key injection, or uploaded file is
not proof of an external application goal.

For delegated Pi work, preserve the task ID, latest durable sequence, task
state, PI WEB session link, terminal event, and relevant artifacts. Treat
`WAITING_INPUT` as a handoff to PI WEB; do not invent an answer endpoint or
flatten Pi's interaction schemas into the host protocol.

## Respect authorization and ownership

PC configuration identifies a target; it does not authorize every mutation.
Read-only inspection may proceed when relevant. Require the user's task to
include installation, deployment, process termination, file deletion,
firewall changes, Rule synchronization, or application-side mutation before
performing that category of change.

Delegation does not broaden authorization. Give Pi a bounded prompt and working
directory consistent with the user's request, follow its progress, and stop or
steer it if it begins materially different work. Do not delegate a mutation
that this skill would not perform directly under the same request.

Do not modify Windows Firewall through this skill. Do not expose the default
unauthenticated HTTP or SFTP listeners to the public Internet.

When a Rule owns the requested domain behavior, do not replace it with
primitive input or an improvised Starlark loop. When the runtime itself is
broken, preserve the reproduction and switch to `maintain-windowsagent-runtime`.
When a Rule or Action is missing or defective, switch to
`develop-windowsagent-rule`.

## Report the result by layer

Separate these claims whenever they apply:

1. **Transport:** the configured PC endpoint received the request.
2. **Runtime:** SFTP, the invocation, or the delegated Pi task reached its
   declared state.
3. **Execution:** the process, script, Action, or Pi session returned the
   expected output.
4. **Desktop/domain:** fresh evidence shows the signed-in session or application
   accepted the effect.
5. **Goal:** the user's full requested outcome is independently confirmed.

Report cleanup and any remaining live dependency. Mention commit, push,
deployment, or publication only when it actually occurred.
