#!/usr/bin/env bash

set -euo pipefail

usage() {
  printf 'Usage: ps1.sh <local-script.ps1> [windows-exec ps1 options]\n' >&2
}

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

local_script="$1"
shift
if [[ ! -f "$local_script" || "$local_script" != *.ps1 ]]; then
  printf 'error: the first argument must be an existing .ps1 file\n' >&2
  exit 2
fi
case "$local_script" in
  *$'\n'*|*'"'*)
    printf 'error: the local script path must not contain a newline or double quote\n' >&2
    exit 2
    ;;
esac
for argument in "$@"; do
  case "$argument" in
    --url|--url=*|--script-path|--script-path=*)
      printf 'error: %s is owned by the single-host staging adapter\n' "$argument" >&2
      exit 2
      ;;
  esac
done

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck disable=SC1091
source "$script_dir/resolve-pc.sh"

for command_name in python3 mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'error: required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

# This WindowsAgent-owned abstraction deliberately stays inside the helper: the
# operator configures neither its SFTP spelling nor its native Windows spelling.
stage_name="task-$(date -u +%Y%m%dT%H%M%SZ)-$$-$RANDOM.ps1"
temp_result="$(python3 "$script_dir/windowsagent_client.py" \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" exec run \
  --executable 'C:\Windows\System32\cmd.exe' \
  --arg /d --arg /c --arg 'set TEMP')"
windows_stage_dir="$(printf '%s' "$temp_result" | python3 -c '
import json, pathlib, sys
result = json.load(sys.stdin)
output = result.get("output", {})
if result.get("state") != "COMPLETED" or output.get("exitCode") != 0:
    raise SystemExit("error: could not query the Windows execution user temporary directory")
values = [line[5:] for line in output.get("stdout", {}).get("text", "").splitlines() if line.upper().startswith("TEMP=")]
if len(values) != 1:
    raise SystemExit("error: Windows TEMP response must contain exactly one value")
path = pathlib.PureWindowsPath(values[0])
if not path.is_absolute() or len(path.drive) != 2 or any(c in values[0] for c in "\r\n\""):
    raise SystemExit("error: Windows TEMP must be an absolute drive path safe for SFTP")
print(str(path))
')"
sftp_stage_dir="$(printf '%s' "$windows_stage_dir" | python3 -c 'import sys; print("/" + sys.stdin.read().replace(chr(92), "/"))')"
sftp_script_path="$sftp_stage_dir/$stage_name"
windows_script_path="$windows_stage_dir\\$stage_name"
upload_batch="$(mktemp "${TMPDIR:-/tmp}/windowsagent-sftp-upload.XXXXXX")"
cleanup_batch="$(mktemp "${TMPDIR:-/tmp}/windowsagent-sftp-cleanup.XXXXXX")"

remote_staged=0
cleanup() {
  status=$?
  trap - EXIT
  cleanup_failed=0
  if [[ "$remote_staged" -eq 1 ]]; then
    printf 'rm "%s"\n' "$sftp_script_path" >"$cleanup_batch"
    if ! "$script_dir/sftp.sh" -q -b "$cleanup_batch" >/dev/null; then
      printf 'error: failed to remove staged PowerShell file: %s\n' "$sftp_script_path" >&2
      cleanup_failed=1
    fi
  fi
  rm -f "$upload_batch" "$cleanup_batch"
  if [[ "$status" -eq 0 && "$cleanup_failed" -ne 0 ]]; then
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

{
  printf 'put "%s" "%s"\n' "$local_script" "$sftp_script_path"
} >"$upload_batch"
remote_staged=1
"$script_dir/sftp.sh" -q -b "$upload_batch"

python3 "$script_dir/windowsagent_client.py" \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  exec ps1 \
  --script-path "$windows_script_path" \
  "$@"
