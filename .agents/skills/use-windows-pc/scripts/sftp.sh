#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck disable=SC1091
source "$script_dir/resolve-pc.sh"

command -v sftp >/dev/null 2>&1 || {
  printf 'error: required command is unavailable: sftp\n' >&2
  exit 1
}

sftp_operation_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -q)
      sftp_operation_args+=("$1")
      shift
      ;;
    -b)
      if [[ $# -lt 2 || -z "$2" ]]; then
        printf 'error: -b requires a batch file\n' >&2
        exit 2
      fi
      sftp_operation_args+=("$1" "$2")
      shift 2
      ;;
    *)
      printf 'error: unsupported SFTP operation argument: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

mkdir -p "$(dirname -- "$WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE")"
touch "$WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE"
chmod 600 "$WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE"

sftp_connection_args=(
  -o PreferredAuthentications=none
  -o PubkeyAuthentication=no
  -o PasswordAuthentication=no
  -o ConnectTimeout=5
  -o StrictHostKeyChecking=accept-new
  -o UserKnownHostsFile="$WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE"
  -P "$WINDOWS_AGENT_SFTP_PORT"
)
if [[ "${#sftp_operation_args[@]}" -eq 0 ]]; then
  exec sftp "${sftp_connection_args[@]}" "$WINDOWS_AGENT_SFTP_USER@$WINDOWS_AGENT_SFTP_HOST"
fi
exec sftp "${sftp_connection_args[@]}" "${sftp_operation_args[@]}" "$WINDOWS_AGENT_SFTP_USER@$WINDOWS_AGENT_SFTP_HOST"
