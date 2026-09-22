#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] submit --cwd <windows-path> --prompt <text>
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] status --task-id <id>
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] watch --task-id <id> [--after <cursor>]
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] message --task-id <id> --mode steer|followUp --text <text>
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] cancel --task-id <id>
  windows-agent-pi-client.sh --ssh-host <user@windows-host> [options] web --task-id <id>

Options:
  --ssh-host <host>       Windows OpenSSH destination.
  --local-port <port>     Local delegated API tunnel port (default: 18791).
  --agent-port <port>     Windows delegated API loopback port (default: 8791).
  --local-web-port <port> Local PI WEB tunnel port (default: 18504).
  --web-port <port>       Windows PI WEB loopback port (default: 8504).
  -h, --help              Show this help.

The client reads the default installer-owned token through SSH without printing
it. The web command opens the PI WEB session and keeps both tunnels alive until
Control-C.
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

is_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && (( "$1" >= 1 && "$1" <= 65535 ))
}

ssh_host="${WINDOWS_AGENT_PI_SSH_HOST:-}"
local_port=18791
agent_port=8791
local_web_port=18504
web_port=8504

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh-host)
      require_value "$@"
      ssh_host="$2"
      shift 2
      ;;
    --local-port)
      require_value "$@"
      local_port="$2"
      shift 2
      ;;
    --agent-port)
      require_value "$@"
      agent_port="$2"
      shift 2
      ;;
    --local-web-port)
      require_value "$@"
      local_web_port="$2"
      shift 2
      ;;
    --web-port)
      require_value "$@"
      web_port="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    submit|status|watch|message|cancel|web)
      command_name="$1"
      shift
      break
      ;;
    *)
      fail "unknown argument or command: $1"
      ;;
  esac
done

[[ -n "$ssh_host" ]] || fail "--ssh-host is required (or set WINDOWS_AGENT_PI_SSH_HOST)"
[[ -n "${command_name:-}" ]] || fail "a command is required"
is_port "$local_port" || fail "--local-port must be an integer from 1 to 65535"
is_port "$agent_port" || fail "--agent-port must be an integer from 1 to 65535"
is_port "$local_web_port" || fail "--local-web-port must be an integer from 1 to 65535"
is_port "$web_port" || fail "--web-port must be an integer from 1 to 65535"
[[ "$local_port" -ne "$local_web_port" ]] || fail "local API and Web ports must differ"

task_id=""
cwd=""
prompt=""
message_mode=""
message_text=""
after_sequence=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --task-id)
      require_value "$@"
      task_id="$2"
      shift 2
      ;;
    --cwd)
      require_value "$@"
      cwd="$2"
      shift 2
      ;;
    --prompt)
      require_value "$@"
      prompt="$2"
      shift 2
      ;;
    --mode)
      require_value "$@"
      message_mode="$2"
      shift 2
      ;;
    --text)
      require_value "$@"
      message_text="$2"
      shift 2
      ;;
    --after)
      require_value "$@"
      after_sequence="$2"
      shift 2
      ;;
    *)
      fail "unknown $command_name argument: $1"
      ;;
  esac
done

case "$command_name" in
  submit)
    [[ -n "$cwd" ]] || fail "submit requires --cwd"
    [[ -n "$prompt" ]] || fail "submit requires --prompt"
    ;;
  status|watch|cancel|web)
    [[ "$task_id" =~ ^task_[0-9a-f]{32}$ ]] || fail "$command_name requires a canonical --task-id"
    ;;
  message)
    [[ "$task_id" =~ ^task_[0-9a-f]{32}$ ]] || fail "message requires a canonical --task-id"
    [[ "$message_mode" == "steer" || "$message_mode" == "followUp" ]] || fail "message requires --mode steer or followUp"
    [[ -n "$message_text" ]] || fail "message requires --text"
    ;;
esac
[[ "$after_sequence" =~ ^[0-9]+$ ]] || fail "--after must be a non-negative integer"

for required_command in ssh curl python3 mktemp; do
  command -v "$required_command" >/dev/null 2>&1 || fail "required command is unavailable: $required_command"
done
if [[ "$command_name" == "web" ]]; then
  command -v open >/dev/null 2>&1 || fail "the macOS open command is unavailable"
fi

umask 077
temporary_dir="$(mktemp -d /tmp/windows-agent-pi.XXXXXX)"
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

printf 'Opening windows-agent-pi tunnel through %s...\n' "$ssh_host" >&2
ssh \
  -M \
  -S "$control_socket" \
  -o ExitOnForwardFailure=yes \
  -fN \
  -L "${local_port}:127.0.0.1:${agent_port}" \
  -L "${local_web_port}:127.0.0.1:${web_port}" \
  "$ssh_host"
tunnel_started=1

delegated_token="$(
  ssh -S "$control_socket" "$ssh_host" \
    'cmd.exe /d /s /c type "%LOCALAPPDATA%\gameGuide\windows-agent-pi\delegated-api.token"' |
    tr -d '\r\n'
)"
[[ -n "$delegated_token" ]] || fail "Windows delegated API token is empty or unavailable"
printf 'Authorization: Bearer %s\n' "$delegated_token" >"$auth_header_file"
unset delegated_token

base_url="http://127.0.0.1:${local_port}"
curl_json() {
  curl --silent --show-error --fail --header "@$auth_header_file" --header 'Content-Type: application/json' "$@"
}

case "$command_name" in
  submit)
    python3 -c 'import json,sys; json.dump({"prompt":sys.argv[1],"cwd":sys.argv[2]},sys.stdout)' "$prompt" "$cwd" |
      curl_json --request POST --data-binary @- "${base_url}/v1/delegated-tasks"
    ;;
  status)
    curl_json "${base_url}/v1/delegated-tasks/${task_id}"
    ;;
  watch)
    printf 'Following delegated task %s after cursor %s; press Control-C to stop.\n' "$task_id" "$after_sequence" >&2
    curl --silent --show-error --fail --no-buffer --header "@$auth_header_file" \
      "${base_url}/v1/delegated-tasks/${task_id}/events/stream?after=${after_sequence}"
    ;;
  message)
    python3 -c 'import json,sys; json.dump({"text":sys.argv[1],"mode":sys.argv[2]},sys.stdout)' "$message_text" "$message_mode" |
      curl_json --request POST --data-binary @- "${base_url}/v1/delegated-tasks/${task_id}/messages"
    ;;
  cancel)
    curl_json --request POST "${base_url}/v1/delegated-tasks/${task_id}/cancel"
    ;;
  web)
    task_json="$(curl_json "${base_url}/v1/delegated-tasks/${task_id}")"
    session_id="$(python3 -c 'import json,sys; value=json.load(sys.stdin); session=value.get("sessionId"); assert isinstance(session,str) and session; print(session)' <<<"$task_json")"
    web_url="http://127.0.0.1:${local_web_port}/?session=$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1],safe=""))' "$session_id")&view=chat"
    open "$web_url"
    printf 'PI WEB tunnel is active at %s; press Control-C to close it.\n' "$web_url" >&2
    while ssh -S "$control_socket" -O check "$ssh_host" >/dev/null 2>&1; do
      sleep 1
    done
    ;;
esac
