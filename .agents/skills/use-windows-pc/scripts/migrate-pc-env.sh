#!/usr/bin/env bash

set -euo pipefail

pc_env="${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}"
if [[ ! -f "$pc_env" ]]; then
  printf 'error: WindowsAgent PC environment does not exist: %s\n' "$pc_env" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$pc_env"
set +a

host="${WINDOWS_AGENT_HOST:-}"
if [[ -z "$host" ]]; then
  legacy_origin="${WINDOWS_AGENT_HTTP_ORIGIN:-}"
  if [[ -z "$legacy_origin" ]]; then
    printf 'error: legacy PC environment has neither WINDOWS_AGENT_HOST nor an HTTP origin\n' >&2
    exit 1
  fi
  host="$(python3 - "$legacy_origin" 2>/dev/null <<'PY'
import sys
import urllib.parse

parsed = urllib.parse.urlsplit(sys.argv[1])
if (
    parsed.scheme != "http"
    or not parsed.hostname
    or parsed.port != 8787
    or parsed.username
    or parsed.password
    or parsed.path not in ("", "/")
    or parsed.query
    or parsed.fragment
):
    raise SystemExit(1)
print(parsed.hostname)
PY
)" || {
    printf 'error: legacy HTTP origin cannot be migrated to the standard host contract\n' >&2
    exit 1
  }
fi

legacy_sftp_host="${WINDOWS_AGENT_SFTP_HOST:-}"
legacy_sftp_host_lower="$(printf '%s' "$legacy_sftp_host" | tr '[:upper:]' '[:lower:]')"
host_lower="$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')"
if [[ -n "$legacy_sftp_host" && "$legacy_sftp_host_lower" != "$host_lower" ]]; then
  printf 'error: legacy HTTP and SFTP hosts disagree; migrate the target explicitly\n' >&2
  exit 1
fi
if [[ -n "${WINDOWS_AGENT_SFTP_PORT:-}" && "$WINDOWS_AGENT_SFTP_PORT" != 2022 ]]; then
  printf 'error: legacy SFTP port is not the WindowsAgent protocol default\n' >&2
  exit 1
fi
if [[ -n "${WINDOWS_AGENT_SFTP_USER:-}" && "$WINDOWS_AGENT_SFTP_USER" != windowsagent ]]; then
  printf 'error: legacy SFTP user is not the WindowsAgent protocol default\n' >&2
  exit 1
fi

pc_env_dir="$(cd -- "$(dirname -- "$pc_env")" && pwd -P)"
temporary="$(mktemp "$pc_env_dir/.pc.env.migrate.XXXXXX")"
cleanup() { rm -f "$temporary"; }
trap cleanup EXIT
umask 077
{
  printf 'WINDOWS_AGENT_HOST=%q\n' "$host"
  for name in WINDOWS_AGENT_PC_NAME WINDOWS_AGENT_EVENT_WEB_ORIGIN WINDOWS_AGENT_ADMIN_SSH_HOST WINDOWS_AGENT_PI_SSH_HOST; do
    value="${!name:-}"
    [[ -z "$value" ]] || printf '%s=%q\n' "$name" "$value"
  done
} >"$temporary"
chmod 600 "$temporary"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
WINDOWS_AGENT_PC_ENV="$temporary" bash -c 'source "$1"' _ "$script_dir/resolve-pc.sh"
mv -f "$temporary" "$pc_env"
trap - EXIT
printf 'WindowsAgent PC environment migrated to the single-host contract.\n'
