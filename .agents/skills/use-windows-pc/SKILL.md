---
name: use-windows-pc
description: "Operate one user-configured Windows PC through stable WindowsAgent capture, file transfer, structured process execution, bounded key and pointer input, or installed Rule Actions. Use for normal end-user Windows inspection and operation. Read only PC identity from the private user env file and derive normal endpoints from that host; report product defects instead of requiring repository-maintainer workflows."
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
- **One literal mouse click:** after a fresh native-density capture, use the
  bundled portable client's `pointer click` with native primary-screen `x/y`
  and the exact foreground PID/name/path. `pointer click-current-position`
  clicks without moving the cursor. A matched Rule is not required.
- **Game- or application-owned behavior:** start from a fresh matched capture,
  read the live Rule guidance and Action catalog, and invoke the highest-level
  Action whose postcondition owns the goal.

SSH is not a fallback execution or file plane.

An unmatched executable does not disable direct keyboard or pointer input.
Do not require creating or deploying a Rule for an explicitly requested
literal key press or visible-button click. A separately installed experimental
`use-windows-starlark` Skill can author bounded sequences of these inputs and
cancellable waits without a Rule; it is optional and not an onboarding
prerequisite.

Read [references/operations.md](references/operations.md) for conventional
WindowsAgent command forms.

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

## Respect authorization and ownership

PC configuration identifies a target; it does not authorize every mutation.
Read-only inspection may proceed when relevant. Require the user's task to
include installation, deployment, process termination, file deletion,
firewall changes, Rule synchronization, or application-side mutation before
performing that category of change.

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
2. **Runtime:** SFTP or the invocation reached its declared state.
3. **Execution:** the process, script, or Action returned the expected output.
4. **Desktop/domain:** fresh evidence shows the signed-in session or application
   accepted the effect.
5. **Goal:** the user's full requested outcome is independently confirmed.

Report cleanup and any remaining live dependency. Mention commit, push,
deployment, or publication only when it actually occurred.
