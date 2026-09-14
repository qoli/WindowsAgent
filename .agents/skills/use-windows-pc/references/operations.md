# Windows PC operation forms

Load `pc.env` as described in [pc-env.md](pc-env.md) before using these forms.

## Health and fresh desktop capture

```bash
curl --fail --silent --show-error \
  "$WINDOWS_AGENT_HTTP_ORIGIN/healthz"

python3 "$WINDOWS_AGENT_CAPTURE_HELPER" --json
```

Use the returned `png_path` when visual inspection is required. Preserve the
capture ID, timestamp, foreground executable, Rule status, and artifact
metadata in acceptance evidence.

## SFTP filesystem operations

Verify the server identity before accepting or using a new entry:

```bash
observed_fingerprint="$(
  ssh-keyscan -T 5 -p "$WINDOWS_AGENT_SFTP_PORT" -t ed25519 \
    "$WINDOWS_AGENT_SFTP_HOST" 2>/dev/null |
  ssh-keygen -lf - -E sha256 |
  awk 'NR == 1 { print $2 }'
)"

test "$observed_fingerprint" = "$WINDOWS_AGENT_SFTP_HOST_KEY_SHA256"
```

Connect only through the SFTP subsystem and SSH `none` authentication:

```bash
mkdir -p "$(dirname "$WINDOWS_AGENT_SFTP_KNOWN_HOSTS")"

sftp \
  -o PreferredAuthentications=none \
  -o PubkeyAuthentication=no \
  -o PasswordAuthentication=no \
  -o StrictHostKeyChecking=accept-new \
  -o UserKnownHostsFile="$WINDOWS_AGENT_SFTP_KNOWN_HOSTS" \
  -P "$WINDOWS_AGENT_SFTP_PORT" \
  "$WINDOWS_AGENT_SFTP_USER@$WINDOWS_AGENT_SFTP_HOST"
```

Virtual `/` contains Windows drive roots. Use the configured SFTP staging path
for temporary scripts and payloads rather than guessing a Windows profile.
Verify important transfers with a digest on both sides or by downloading and
comparing the exact bytes. Clean up only files created for the authorized task.

## Direct process execution

Run the console client from its Go module root:

```bash
cd "$WINDOWS_AGENT_REPO"

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

Use `--stdin-file` for exact stdin bytes. Use `--max-output-bytes` only when the
task has a real output bound. A nonzero child exit code is result data; a typed
`EXEC_*` error is a runtime failure.

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

Upload the script through SFTP to `WINDOWS_AGENT_SFTP_STAGING_DIR`. Then invoke
the native path for the same file:

```bash
cd "$WINDOWS_AGENT_REPO"

go run ./cmd/windows-exec ps1 \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --script-path "$WINDOWS_AGENT_WINDOWS_STAGING_DIR\\task.ps1" \
  --arg -Mode \
  --arg Inspect
```

The runtime uses built-in Windows PowerShell 5.1 with `-File`. Do not silently
select `pwsh`, inline the file with `-Command`, or fall back to SSH execution.
Do not upload a PowerShell P/Invoke or `SendInput` script to approximate a key
Action.

## Direct foreground-pinned key press

Obtain a fresh capture immediately before the press, then pass its exact
foreground identity without deriving it from a title or process inventory:

```bash
cd "$WINDOWS_AGENT_REPO"

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
cd "$WINDOWS_AGENT_REPO"

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
