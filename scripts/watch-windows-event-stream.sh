#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  watch-windows-event-stream.sh --ssh-host <user@windows-host> [options]

Options:
  --ssh-host <host>   Windows OpenSSH destination, for example user@Windows-PC.
  --tail <count>      Replay this many recent events before following (default: 10).
  --after <sequence>  Replay after this exact sequence instead of using --tail.
  --local-port <port> Local tunnel port (default: 18788).
  --event-port <port> Windows event-stream loopback port (default: 8788).
  -h, --help          Show this help.

The script opens an SSH tunnel, reads the installed event API token without
printing it, and follows the NDJSON endpoint until interrupted with Control-C.
EOF
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_value() {
  if [[ $# -lt 2 || -z "$2" ]]; then
    fail "$1 requires a value"
  fi
}

is_nonnegative_integer() {
  [[ "$1" =~ ^[0-9]+$ ]]
}

ssh_host="${WINDOWS_EVENT_SSH_HOST:-}"
tail_count=10
after_sequence=""
local_port=18788
event_port=8788

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh-host)
      require_value "$@"
      ssh_host="$2"
      shift 2
      ;;
    --tail)
      require_value "$@"
      tail_count="$2"
      shift 2
      ;;
    --after)
      require_value "$@"
      after_sequence="$2"
      shift 2
      ;;
    --local-port)
      require_value "$@"
      local_port="$2"
      shift 2
      ;;
    --event-port)
      require_value "$@"
      event_port="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ -n "$ssh_host" ]] || fail "--ssh-host is required (or set WINDOWS_EVENT_SSH_HOST)"
is_nonnegative_integer "$tail_count" || fail "--tail must be a non-negative integer"
[[ -z "$after_sequence" ]] || is_nonnegative_integer "$after_sequence" || fail "--after must be a non-negative integer"
is_nonnegative_integer "$local_port" || fail "--local-port must be an integer from 1 to 65535"
is_nonnegative_integer "$event_port" || fail "--event-port must be an integer from 1 to 65535"
(( local_port >= 1 && local_port <= 65535 )) || fail "--local-port must be an integer from 1 to 65535"
(( event_port >= 1 && event_port <= 65535 )) || fail "--event-port must be an integer from 1 to 65535"

for command_name in ssh curl python3 mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required command is unavailable: $command_name"
done

umask 077
temporary_dir="$(mktemp -d /tmp/windows-event-stream.XXXXXX)"
control_socket="$temporary_dir/ssh-control"
auth_header_file="$temporary_dir/authorization-header"
tunnel_started=0

cleanup() {
  if [[ "$tunnel_started" -eq 1 ]]; then
    ssh -S "$control_socket" -O exit "$ssh_host" >/dev/null 2>&1 || true
  fi
  rm -f "$auth_header_file" "$control_socket"
  rmdir "$temporary_dir" >/dev/null 2>&1 || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 143' TERM

printf 'Opening Windows event-stream tunnel through %s...\n' "$ssh_host" >&2
ssh \
  -M \
  -S "$control_socket" \
  -o ExitOnForwardFailure=yes \
  -fN \
  -L "${local_port}:127.0.0.1:${event_port}" \
  "$ssh_host"
tunnel_started=1

event_token="$(
  ssh -S "$control_socket" "$ssh_host" \
    'cmd.exe /d /s /c type "%LOCALAPPDATA%\gameGuide\windows-capture-agent\event-api.token"' |
    tr -d '\r\n'
)"
[[ -n "$event_token" ]] || fail "Windows event API token is empty or unavailable"
printf 'Authorization: Bearer %s\n' "$event_token" >"$auth_header_file"
unset event_token

base_url="http://127.0.0.1:${local_port}"

if [[ -z "$after_sequence" ]]; then
  replay_status="$(
    curl \
      --silent \
      --show-error \
      --fail \
      --header "@$auth_header_file" \
      "${base_url}/v1/events?after=0&limit=1"
  )"
  last_sequence="$(
    python3 -c \
      'import json, sys; value = json.load(sys.stdin)["lastSequence"]; assert isinstance(value, int) and value >= 0; print(value)' \
      <<<"$replay_status"
  )"
  if (( last_sequence > tail_count )); then
    after_sequence=$(( last_sequence - tail_count ))
  else
    after_sequence=0
  fi
fi

printf 'Following NDJSON events after sequence %s; press Control-C to stop.\n' "$after_sequence" >&2
curl \
  --silent \
  --show-error \
  --fail \
  --no-buffer \
  --header "@$auth_header_file" \
  "${base_url}/v1/events/stream?after=${after_sequence}"
