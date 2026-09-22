---
name: use-windows-pc
description: "Operate one user-configured Windows PC through stable WindowsAgent capture, file transfer, structured process execution, bounded key input, installed Rule Actions, or the optional delegated Pi agent. Use for normal end-user Windows inspection and operation. Read only PC identity from the private user env file and derive normal endpoints from that host; report product defects instead of requiring repository-maintainer workflows."
---

# Use Windows PC

Operate one explicitly configured Windows PC without teaching the public skill
which private host it is. Resolve one host identity from the user-owned env
file, derive normal service addresses, and use only clients shipped inside this
skill before selecting the narrowest WindowsAgent capability that owns the
requested result.

## Resolve the configured PC

The canonical configuration is:

```text
${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}
```

Read [references/pc-env.md](references/pc-env.md) before the first operation in
a task. Source `scripts/resolve-pc.sh`; it loads the file without printing it
and derives the normal HTTP and SFTP service addresses, fixed SFTP protocol
user, and local known-hosts state. Do not assemble those values
from examples, previous tasks, SSH config, or visible Windows content.

If the env file or `WINDOWS_AGENT_HOST` is absent and the user supplied a host
through the AssistGUI setup prompt, create the private configuration as
described in [references/setup.md](references/setup.md), then continue through
initial acceptance. Otherwise report the missing dependency explicitly. Do
not switch to another PC or transport.

The env file is private operator configuration. Never print its complete
contents, commit it, copy it into a Rule, or place its machine-specific values
in this skill.

## Select the owning capability

Use only the capability needed for the requested outcome:

- **Current desktop and foreground:** use the bundled `scripts/capture.sh`. A
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
- **Independent delegated work:** use `windows-agent-pi` when the Windows-side
  worker should plan, inspect, iterate, use its own workspace and computer-use,
  and stream durable progress back to the supervising host. Pi SDK/pi-ai owns
  model execution, PI WEB owns the session and human interaction surface, and
  `windows-agent-pi` owns the normalized delegated-task lifecycle. This is not
  a WindowsAgent Action or a fallback for another failed capability.
- **Game- or application-owned behavior:** start from a fresh matched capture,
  read the live Rule guidance and Action catalog, and invoke the highest-level
  Action whose postcondition owns the goal.

SSH is not a fallback execution or file plane. The optional Pi SSH target is
used only to tunnel the loopback delegated API and PI WEB.

Read [references/operations.md](references/operations.md) for conventional
WindowsAgent command forms. Read
[references/pi-agent.md](references/pi-agent.md) before submitting, following,
steering, cancelling, or opening a delegated Pi task.

## Establish live state

Before a state-dependent operation:

1. Source the bundled resolver and require its single configured host.
2. Require `GET $WINDOWS_AGENT_HTTP_ORIGIN/healthz` to return `status: ok`.
3. Request a new capture through the bundled `scripts/capture.sh`.
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

For SFTP, use the bundled `scripts/sftp.sh`. It persists the first observed
server key in Harness-local known-hosts state and rejects a later key change.
No fingerprint is pre-provisioned in PC configuration. Authentication must be
SSH `none` with the fixed protocol username; do not try local keys or passwords
as an alternate path.

For `run`, `ps1`, direct key input, and Actions, preserve the
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
primitive input or improvised automation. If the runtime is defective,
preserve the smallest useful reproduction and report a WindowsAgent runtime
defect. If a Rule or Action is missing or defective, report the capability as
unavailable or defective. This end-user Skill does not require or route into
repository development Skills.

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
