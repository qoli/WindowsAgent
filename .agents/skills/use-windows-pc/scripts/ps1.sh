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

for command_name in go mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'error: required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  }
done

# This WindowsAgent-owned abstraction deliberately stays inside the helper: the
# operator configures neither its SFTP spelling nor its native Windows spelling.
stage_name="task-$(date -u +%Y%m%dT%H%M%SZ)-$$-$RANDOM.ps1"
sftp_stage_dir='/c:/Windows/Temp/WindowsAgentHarness'
windows_stage_dir='C:\Windows\Temp\WindowsAgentHarness'
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
  printf -- '-mkdir "%s"\n' "$sftp_stage_dir"
  printf 'put "%s" "%s"\n' "$local_script" "$sftp_script_path"
} >"$upload_batch"
remote_staged=1
"$script_dir/sftp.sh" -q -b "$upload_batch"

cd "$WINDOWS_AGENT_HARNESS_ROOT"
go run ./cmd/windows-exec ps1 \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  --script-path "$windows_script_path" \
  "$@"
