#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
for argument in "$@"; do
  case "$argument" in
    --agent-url|--agent-url=*|--ssh-host|--ssh-host=*|--ssh-user|--ssh-user=*|\
    --ssh-port|--ssh-port=*|--ssh-timeout|--ssh-timeout=*|\
    --restart-timeout|--restart-timeout=*|--recovery-python|--recovery-python=*)
      printf 'error: %s is owned by the single-host capture adapter\n' "$argument" >&2
      exit 2
      ;;
  esac
done
# shellcheck disable=SC1091
source "$script_dir/resolve-pc.sh"

exec python3 "$script_dir/windowsagent_client.py" \
  --url "$WINDOWS_AGENT_HTTP_ORIGIN" \
  capture \
  "$@"
