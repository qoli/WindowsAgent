# Delegated Pi agent operations

Use this mode when the configured Windows PC should run a general task as an
independent supervised agent. Pi SDK/pi-ai supplies model execution,
`pi-computer-use` owns its desktop interaction, PI WEB owns the detailed
session and user interactions, and `windows-agent-pi` projects a stable durable
task stream to the macOS host.

This mode is not a primitive input API, a Rule Action, or a hidden fallback for
a failed WindowsAgent capability.

## Load the configured transport

Resolve the configured PC as described in [pc-env.md](pc-env.md), then require
the capability-specific `WINDOWS_AGENT_PI_SSH_HOST`. Do not guess another SSH
target or Windows path.

```bash
skill_root="${CODEX_HOME:-$HOME/.codex}/skills/use-windows-pc"
source "$skill_root/scripts/resolve-pc.sh"
pi_client="$skill_root/scripts/windows-agent-pi-client.sh"
test -x "$pi_client"
test -n "$WINDOWS_AGENT_PI_SSH_HOST"
```

The client opens SSH tunnels to the loopback-only delegated API and PI WEB. It
reads the installer-owned bearer token without printing it and removes its
temporary authorization file when the command exits. Do not expose ports 8791
or 8504 directly to an untrusted network.

## Submit and follow a task

Submit one bounded prompt with an explicit absolute Windows working directory:

```bash
"$pi_client" submit \
  --cwd 'C:\absolute\workspace' \
  --prompt 'Inspect this workspace, complete the requested change, run the relevant checks, and report the exact result.'
```

Preserve the returned `taskId`, `sessionId`, `piWebUrl`, state, and latest
durable sequence. The working directory limits context; it does not create an
authorization sandbox. The prompt must stay within the user's authorized
operations and should name the intended result and evidence rather than
prescribing low-level computer input.

Inspect a snapshot or follow the durable stream:

```bash
"$pi_client" status --task-id "$task_id"
"$pi_client" watch --task-id "$task_id" --after "$last_sequence"
```

`watch` emits newline-delimited durable events and remains attached until the
stream ends or the caller interrupts it. Resume with the last observed host
sequence; host sequences are not PI WEB WebSocket sequences. Keep the task ID
and cursor together when reporting progress.

## Guide, interact, or cancel

While a task is running, send `steer` when the active work needs redirection or
`followUp` when another turn should run after the current work:

```bash
"$pi_client" message --task-id "$task_id" --mode steer \
  --text 'Keep the change scoped to the delegated runtime and preserve unrelated files.'

"$pi_client" message --task-id "$task_id" --mode followUp \
  --text 'After the tests pass, summarize the changed files and remaining risks.'
```

When the task enters `WAITING_INPUT`, open its PI WEB session:

```bash
"$pi_client" web --task-id "$task_id"
```

The `web` command opens the session in the macOS browser and keeps both tunnels
alive until interrupted. PI WEB is the only answer path for questions and
extension dialogs; the delegated API intentionally does not reproduce those
schemas. Its URL is local to the active tunnel and is not a durable public
link.

Cancel only when the user's request authorizes stopping that work or the task
has diverged from its permitted scope:

```bash
"$pi_client" cancel --task-id "$task_id"
```

Follow cancellation until a terminal `CANCELLED` or `FAILED` state. The cancel
request alone is not proof that Pi and its child work stopped.

## Interpret lifecycle and evidence

Task states are `STARTING`, `RUNNING`, `WAITING_INPUT`, `CANCELLING`,
`COMPLETED`, `FAILED`, and `CANCELLED`. Preserve typed failures and do not
resubmit through another model, computer-use backend, transport, or Action
unless the user explicitly chooses a new attempt.

`COMPLETED` proves that the Pi run ended successfully at its own lifecycle
boundary. It does not independently prove a visible desktop result, file
mutation, build, deployment, publication, or external side effect. Confirm the
user's actual goal with the PI WEB transcript and artifacts, repository or
filesystem state, a fresh capture, or an owning Rule Action postcondition as
appropriate.
