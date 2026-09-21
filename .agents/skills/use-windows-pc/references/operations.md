# Windows PC operation forms

Resolve the one configured PC before using these forms:

```bash
skill_root="${CODEX_HOME:-$HOME/.codex}/skills/use-windows-pc"
source "$skill_root/scripts/resolve-pc.sh"
```

The resolver requires only `WINDOWS_AGENT_HOST` from PC identity. It derives
the normal HTTP origin, SFTP host/port/fixed user, Harness root, and local
known-hosts state.

## Health and fresh desktop capture

```bash
curl --fail --silent --show-error \
  "$WINDOWS_AGENT_HTTP_ORIGIN/healthz"

"$skill_root/scripts/capture.sh"
```

The maintained capture helper creates a current capture, downloads it through
its owned recovery-aware workflow, and emits the capture metadata plus local
`image_path`. Preserve its capture ID, timestamp, foreground executable, Rule
status, and artifact metadata in acceptance evidence. If it fails, report its
exact error; do not replace it with direct HTTP calls or an older image.

## SFTP filesystem operations

Use the bundled wrapper:

```bash
"$skill_root/scripts/sftp.sh"
```

The wrapper derives the host, port `2022`, and fixed protocol user
`windowsagent`. It uses SSH `none` authentication, automatically records the
first observed server key in Harness-local known-hosts state, and rejects a
later key change. No fingerprint or known-hosts path is provisioned in
`pc.env`.

Virtual `/` contains Windows drive roots. Verify important transfers with a
digest on both sides or by downloading and comparing the exact bytes. Clean up
only files created for the authorized task.

## Direct process execution

Run the bundled console client from the Harness root:

```bash
cd "$WINDOWS_AGENT_HARNESS_ROOT"

go run ./cmd/windows-exec run \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --executable 'C:\Windows\System32\whoami.exe'
```

Keep every process argument distinct:

```bash
go run ./cmd/windows-exec run \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --executable 'C:\Path\tool.exe' \
  --arg first \
  --arg 'value with spaces' \
  --env MODE=inspect \
  --cwd 'C:\WorkingDirectory' \
  --timeout 30s
```

Use `--stdin-file` for exact stdin bytes. Use `--max-output-bytes` only when
the task has a real output bound. A nonzero child exit code is result data; a
typed `EXEC_*` error is a runtime failure.

For creation-only work:

```bash
go run ./cmd/windows-exec start \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --executable 'C:\Path\Application.exe' \
  --window normal
```

`start` returns after process creation and has no stop target. If the requested
outcome is visible or application-specific, obtain fresh independent evidence
afterward.

## Uploaded PowerShell file

Use the bundled staging adapter with one local script path:

```bash
"$skill_root/scripts/ps1.sh" /absolute/local/task.ps1 \
  --arg -Mode \
  --arg Inspect
```

The adapter owns the temporary SFTP and native-Windows path mapping, uploads
the script, invokes `windows-exec ps1`, and removes the staged copy. Neither
spelling of the staging directory is part of PC configuration or an operator
input.

The runtime uses built-in Windows PowerShell 5.1 with `-File`. Do not silently
select `pwsh`, inline the file with `-Command`, or fall back to SSH execution.
Do not upload a PowerShell P/Invoke or `SendInput` script to approximate a key
Action.

## Direct foreground-pinned key press

Obtain a fresh capture immediately before the press, then pass its exact
foreground identity without deriving it from a title or process inventory:

```bash
cd "$WINDOWS_AGENT_HARNESS_ROOT"

go run ./cmd/windows-key press \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --key Key_Home \
  --hold 180ms \
  --expected-process-id 1234 \
  --expected-executable-name Game.exe \
  --expected-executable-path 'C:\Games\Game.exe'
```

Use only canonical `Key_*` names accepted by the runtime. The client follows
the durable Action invocation to a terminal state. `COMPLETED` proves the
bounded scan-code press and release only; capture again when visible or domain
acceptance matters. Foreground drift, an active lease conflict, injection
failure, and release failure are terminal. Do not switch to a PowerShell,
SSH, virtual-key, or window-message input path.

## Starlark automation

For a general multi-step workflow, author a package under a task-local
directory and invoke it with:

```bash
cd "$WINDOWS_AGENT_HARNESS_ROOT"

go run ./cmd/windows-starlark-invoke \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --package /absolute/path/to/package \
  --inputs /absolute/path/to/inputs.json
```

The local client proves package, syntax, schema, and inputs preflight. Follow
the returned invocation status or watch URL to a durable terminal state.
Runtime paths, permissions, processes, desktop state, and postconditions can be
answered only by the configured Windows PC.

## Rule Actions

After a fresh capture reports a matched Rule, follow the returned Rule guidance
and catalog URLs. Invoke the highest-level existing Action rather than guessing
an ID from repository history:

```bash
curl --fail --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @/absolute/path/to/action-request.json \
  "$WINDOWS_AGENT_HTTP_ORIGIN/v1/actions/invoke"
```

Finite Actions require a terminal `COMPLETED` response. Streaming Actions
require following the returned durable watch stream to exactly one terminal
state. Action completion is not the external goal unless the declared output
contains its independent domain postcondition.
