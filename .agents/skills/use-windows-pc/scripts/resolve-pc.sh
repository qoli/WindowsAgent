#!/usr/bin/env bash

# Source this file to load the single-host PC contract and derive the normal
# WindowsAgent service endpoints and local Skill-owned state.

windows_agent_config_error() {
  printf 'error: %s\n' "$*" >&2
  return 1
}

windows_agent_pc_env="${WINDOWS_AGENT_PC_ENV:-$HOME/.config/windowsagent/pc.env}"
if [[ ! -r "$windows_agent_pc_env" ]]; then
  windows_agent_config_error "WindowsAgent PC environment is not readable: $windows_agent_pc_env"
  return 1 2>/dev/null || exit 1
fi

case "$-" in
  *a*) windows_agent_restore_allexport=0 ;;
  *) windows_agent_restore_allexport=1; set -a ;;
esac
unset WINDOWS_AGENT_HOST
# shellcheck disable=SC1090
if ! source "$windows_agent_pc_env"; then
  [[ "$windows_agent_restore_allexport" -eq 0 ]] || set +a
  windows_agent_config_error "could not load WindowsAgent PC environment: $windows_agent_pc_env"
  return 1 2>/dev/null || exit 1
fi
[[ "$windows_agent_restore_allexport" -eq 0 ]] || set +a
unset windows_agent_restore_allexport

if [[ -z "${WINDOWS_AGENT_HOST:-}" ]]; then
  windows_agent_config_error "WINDOWS_AGENT_HOST is required in $windows_agent_pc_env"
  return 1 2>/dev/null || exit 1
fi
if ! windows_agent_normalized_host="$(python3 - "$WINDOWS_AGENT_HOST" 2>/dev/null <<'PY'
import ipaddress
import re
import sys

raw = sys.argv[1]
if raw.startswith("[") or raw.endswith("]"):
    if not (raw.startswith("[") and raw.endswith("]")):
        raise SystemExit("brackets must enclose one IPv6 address")
    inner = raw[1:-1]
    try:
        address = ipaddress.ip_address(inner)
    except ValueError as error:
        raise SystemExit(str(error))
    if address.version != 6:
        raise SystemExit("brackets are accepted only for IPv6")
    host = address.compressed
    print(f"[{host}]")
    print(f"[{host}]")
    raise SystemExit

try:
    address = ipaddress.ip_address(raw)
except ValueError:
    if len(raw) > 253 or not raw:
        raise SystemExit("hostname length is invalid")
    labels = raw[:-1].split(".") if raw.endswith(".") else raw.split(".")
    label = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?")
    if not labels or any(not label.fullmatch(part) for part in labels):
        raise SystemExit("value is not a valid hostname or IP address")
    print(raw)
    print(raw)
else:
    host = address.compressed
    if address.version == 6:
        print(f"[{host}]")
        print(f"[{host}]")
    else:
        print(host)
        print(host)
PY
)"; then
  windows_agent_config_error "WINDOWS_AGENT_HOST must be one hostname or IP address without credentials, scheme, port, path, query, or fragment"
  return 1 2>/dev/null || exit 1
fi
windows_agent_http_host="${windows_agent_normalized_host%%$'\n'*}"
windows_agent_sftp_host="${windows_agent_normalized_host#*$'\n'}"

windows_agent_state_root="${XDG_STATE_HOME:-$HOME/.local/state}/windowsagent"

export WINDOWS_AGENT_HTTP_ORIGIN="http://${windows_agent_http_host}:8787"
export WINDOWS_AGENT_SFTP_HOST="$windows_agent_sftp_host"
export WINDOWS_AGENT_SFTP_PORT=2022
export WINDOWS_AGENT_SFTP_USER=windowsagent
export WINDOWS_AGENT_SFTP_KNOWN_HOSTS_FILE="$windows_agent_state_root/known_hosts"

unset windows_agent_http_host windows_agent_sftp_host windows_agent_normalized_host
unset windows_agent_state_root
